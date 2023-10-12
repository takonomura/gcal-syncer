package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strconv"

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

	client, err := google.DefaultClient(ctx, calendar.CalendarEventsScope)
	if err != nil {
		log.Fatalf("preparing google client: %+v", err)
	}

	svc, err := calendar.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("preparing calendar service: %+v", err)
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
