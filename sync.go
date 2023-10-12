package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"golang.org/x/sync/semaphore"
	"google.golang.org/api/calendar/v3"
)

type Config struct {
	ID string `json:"id"`

	SourceCalendars []struct {
		ID     string `json:"id"`
		Prefix string `json:"prefix"`
	} `json:"source_calendars"`
	TargetCalendarID   string   `json:"target_calendar_id"`
	ExcludeCalendarIDs []string `json:"exclude_calendar_ids"`

	BusyOnly bool   `json:"busy_only,omitempty"`
	Mask     string `json:"mask,omitempty"`
}

type Syncer struct {
	Service *calendar.Service

	Config  Config
	TimeMin string
	TimeMax string

	UpdateConcurrency int64
}

func (s *Syncer) shouldSync(e *calendar.Event) bool {
	return (!s.Config.BusyOnly || e.Transparency == "opaque" || e.Transparency == "") && e.Status != "cancelled"
}

func (s *Syncer) icalUID(e *calendar.Event) string {
	transparency := e.Transparency
	if transparency == "" {
		transparency = "opaque"
	}
	return fmt.Sprintf("%s-%s@%s", e.Id, transparency, s.Config.ID)
}

func (s *Syncer) list(ctx context.Context, calID string, fn func(*calendar.Event) error) error {
	call := s.Service.Events.List(calID).SingleEvents(true)
	if s.TimeMin != "" {
		call = call.TimeMin(s.TimeMin)
	}
	if s.TimeMax != "" {
		call = call.TimeMax(s.TimeMax)
	}

	return call.Pages(ctx, func(events *calendar.Events) error {
		for _, e := range events.Items {
			if err := fn(e); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Syncer) buildEvent(base, original *calendar.Event, prefix string) *calendar.Event {
	if base != nil {
		modified := new(calendar.Event)
		*modified = *base
		modified.Summary = prefix + base.Summary
		return modified
	}

	e := &calendar.Event{
		ICalUID:      s.icalUID(original),
		Start:        original.Start,
		End:          original.End,
		Transparency: original.Transparency,
	}

	if s.Config.Mask == "" {
		e.Summary = original.Summary
		e.Description = original.Description
		e.Location = original.Location
	} else {
		e.Summary = s.Config.Mask
	}

	e.Summary = prefix + e.Summary

	return e
}

func (s *Syncer) equalEvent(a, b *calendar.Event) bool {
	return a.Summary == b.Summary &&
		a.Description == b.Description &&
		a.Location == b.Location &&
		a.Start.Date == b.Start.Date && a.Start.DateTime == b.Start.DateTime &&
		a.End.Date == b.End.Date && a.End.DateTime == b.End.DateTime &&
		a.Transparency == b.Transparency

}

func (s *Syncer) Sync(ctx context.Context) error {
	updates := make(map[string]*calendar.Event)
	for _, cal := range s.Config.SourceCalendars {
		log.Printf("listing source calendar %q", cal.ID)

		err := s.list(ctx, cal.ID, func(e *calendar.Event) error {
			if !s.shouldSync(e) {
				return nil
			}
			id := s.icalUID(e)
			updates[id] = s.buildEvent(updates[id], e, cal.Prefix)
			return nil
		})
		if err != nil {
			return fmt.Errorf("listing calendar %q: %w", cal.ID, err)
		}
	}

	for _, calID := range s.Config.ExcludeCalendarIDs {
		log.Printf("listing exclude calendar %q", calID)

		err := s.list(ctx, calID, func(e *calendar.Event) error {
			id := s.icalUID(e)
			delete(updates, id)
			return nil
		})
		if err != nil {
			return fmt.Errorf("listing calendar %q: %w", calID, err)
		}
	}

	log.Printf("listing target calendar %q", s.Config.TargetCalendarID)
	var deletes []string
	err := s.list(ctx, s.Config.TargetCalendarID, func(e *calendar.Event) error {
		id := e.ICalUID
		if desired, ok := updates[id]; !ok {
			delete(updates, id)
			deletes = append(deletes, e.Id)
		} else if s.equalEvent(desired, e) {
			delete(updates, id)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("listing calendar %q: %w", s.Config.TargetCalendarID, err)
	}

	log.Printf("found %d updated/created events and %d deleted events", len(updates), len(deletes))

	sem := semaphore.NewWeighted(s.UpdateConcurrency)
	var errs []error
	var mu sync.Mutex
	for _, e := range updates {
		if err := sem.Acquire(ctx, 1); err != nil {
			return err
		}
		go func(e *calendar.Event) {
			defer sem.Release(1)

			log.Printf("updating event %q", e.ICalUID)

			_, err := s.Service.Events.Import(s.Config.TargetCalendarID, e).Context(ctx).Do()
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("updating event %q: %w", e.ICalUID, err))
				mu.Unlock()
			}
		}(e)
	}
	for _, id := range deletes {
		if err := sem.Acquire(ctx, 1); err != nil {
			return err
		}
		go func(id string) {
			defer sem.Release(1)

			log.Printf("deleting event %q", id)

			err := s.Service.Events.Delete(s.Config.TargetCalendarID, id).Context(ctx).Do()
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("deleting event %q: %w", id, err))
				mu.Unlock()
			}
		}(id)
	}

	// Wait all updates
	if err := sem.Acquire(ctx, s.UpdateConcurrency); err != nil {
		return err
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}
