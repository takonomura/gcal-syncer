package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/hashicorp/go-retryablehttp"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

func main() {
	ctx := context.Background()

	var config Config
	if err := json.Unmarshal([]byte(os.Getenv("GCAL_SYNCER_CONFIG")), &config); err != nil {
		log.Fatalf("loading config: %+v", err)
	}

	concurrency := int64(10)
	if env := os.Getenv("GCAL_SYNCER_UPDATE_CONCURRENCY"); env != "" {
		i, err := strconv.ParseInt(env, 10, 64)
		if err != nil {
			log.Fatalf("parsing update concurrency: %+v", err)
		}
		concurrency = i
	}

	log.Printf("connecting to Google")
	svc, err := NewCalendarService(ctx)
	if err != nil {
		log.Fatalf("failed to prepare calendar service: %+v", err)
	}

	s := &Syncer{
		Service:           svc,
		Config:            config,
		TimeMin:           os.Getenv("GCAL_SYNCER_TIME_MIN"),
		TimeMax:           os.Getenv("GCAL_SYNCER_TIME_MAX"),
		UpdateConcurrency: concurrency,
	}

	err = s.Sync(ctx)
	if err != nil {
		log.Fatal(err)
	}
}

type retryClientLogger struct{}

func (l *retryClientLogger) Printf(format string, v ...interface{}) {
	if strings.HasPrefix(format, "[DEBUG] ") {
		// Do not print debug logs
		return
	}
	log.Printf(format, v...)
}

func NewCalendarService(ctx context.Context) (*calendar.Service, error) {
	client, err := google.DefaultClient(ctx, calendar.CalendarEventsScope)
	if err != nil {
		return nil, fmt.Errorf("default google client: %w", err)
	}

	retryClient := retryablehttp.NewClient()
	retryClient.HTTPClient = client
	retryClient.Logger = &retryClientLogger{}
	retryClient.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		if err == nil && resp.StatusCode == 403 {
			// Rate Limit Exceeded
			return true, nil
		}
		return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
	}

	svc, err := calendar.NewService(ctx, option.WithHTTPClient(retryClient.StandardClient()))
	if err != nil {
		return nil, fmt.Errorf("new calendar service: %w", err)
	}
	return svc, nil
}
