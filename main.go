package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

func main() {
	ctx := context.Background()

	config, err := LoadConfig()
	if err != nil {
		log.Fatalf("loading config: %+v", err)
	}
	status, err := LoadStatus()
	if err != nil {
		log.Printf("failed to load status (will process full sync): %+v", err)
	}
	if status == nil {
		status = make(map[string]SyncStatus)
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
		Service: svc,
		TimeMin: os.Getenv("GCAL_SYNCER_TIME_MIN"),
		TimeMax: os.Getenv("GCAL_SYNCER_TIME_MAX"),
	}

	for _, c := range config {
		log.Printf("syncing %q", c.ID)

		s, err := s.Sync(ctx, c, status[c.ID])
		if err != nil {
			log.Fatalf("failed to sync %q: %+v", c.ID, err)
		}
		status[c.ID] = s
	}

	if err := DumpStatus(status); err != nil {
		log.Fatalf("saving status: %+v", err)
	}
}

func LoadConfig() ([]SyncConfig, error) {
	f, err := os.Open("config.json")
	if err != nil {
		return nil, fmt.Errorf("opening config file: %w", err)
	}
	defer f.Close()

	var v []SyncConfig
	if err := json.NewDecoder(f).Decode(&v); err != nil {
		return nil, fmt.Errorf("decoding config: %w", err)
	}
	return v, nil
}

func LoadStatus() (map[string]SyncStatus, error) {
	f, err := os.Open("status.json")
	if err != nil {
		return nil, fmt.Errorf("opening status file: %w", err)
	}
	defer f.Close()

	var v map[string]SyncStatus
	if err := json.NewDecoder(f).Decode(&v); err != nil {
		return nil, fmt.Errorf("decoding status: %w", err)
	}
	return v, nil
}

func DumpStatus(v map[string]SyncStatus) error {
	f, err := os.Create("status.json")
	if err != nil {
		return fmt.Errorf("creating status file: %w", err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(v); err != nil {
		return fmt.Errorf("writing status: %w", err)
	}
	return nil
}
