package main

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/api/calendar/v3"
)

type Syncer struct {
	Service *calendar.Service

	TimeMin string
	TimeMax string
}

func (s *Syncer) Sync(ctx context.Context, c SyncConfig, last SyncStatus) (SyncStatus, error) {
	call := s.Service.Events.List(c.SourceCalendarID).Context(ctx).ShowDeleted(true)
	if last.LastModified != "" {
		call = call.UpdatedMin(last.LastModified)
	}
	if s.TimeMin != "" {
		call = call.TimeMin(s.TimeMin)
	}
	if s.TimeMax != "" {
		call = call.TimeMax(s.TimeMax)
	}

	var status SyncStatus
	err := call.Pages(ctx, func(events *calendar.Events) error {
		status = SyncStatus{
			LastModified: events.Updated,
		}
		for _, e := range events.Items {
			if e.RecurringEventId != "" {
				err := s.updateRecurringException(ctx, c, e)
				if err != nil {
					return fmt.Errorf("updating recurring exception event %q: %w", e.Id, err)
				}
				continue
			}
			if e.Status == "cancelled" {
				err := s.deleteEvent(ctx, c.TargetCalendarID, c.ICalUID(e.Id))
				if err != nil {
					return fmt.Errorf("deleting event %q: %w", e.Id, err)
				}
				log.Printf("deleted event %q for %q", e.Id, c.ID)
				continue
			}
			if !c.ShouldSync(e) {
				log.Printf("skipped event %q for %q", e.Id, c.ID)
				continue
			}
			_, err := s.Service.Events.Import(c.TargetCalendarID, c.ConvertEvent(e)).Context(ctx).Do()
			if err != nil {
				return fmt.Errorf("syncing event %q: %w", e.Id, err)
			}
			log.Printf("synced event %q for %q", e.Id, c.ID)
		}
		return nil
	})
	return status, err
}

func (s *Syncer) updateRecurringException(ctx context.Context, c SyncConfig, e *calendar.Event) error {
	recurringId, err := s.findEventId(ctx, c.TargetCalendarID, c.ICalUID(e.RecurringEventId))
	if err != nil {
		return fmt.Errorf("finding target event id for %q", e.RecurringEventId)
	}
	if recurringId == "" {
		log.Printf("no parent event for %q is found; skipped", e.Id)
		return nil
	}
	originalStart := e.OriginalStartTime.DateTime
	if originalStart == "" {
		originalStart = e.OriginalStartTime.Date
	}
	err = s.Service.Events.Instances(c.TargetCalendarID, recurringId).OriginalStart(originalStart).Pages(ctx, func(events *calendar.Events) error {
		for _, i := range events.Items {
			i.Start = e.Start
			i.End = e.End
			i.Status = e.Status

			if c.Mask == "" {
				i.Summary = e.Summary
				i.Description = e.Description
				i.Location = e.Location
			} else {
				i.Summary = c.Mask
			}

			_, err := s.Service.Events.Update(c.TargetCalendarID, i.Id, i).Context(ctx).Do()
			if err != nil {
				return fmt.Errorf("updating event %q: %w", i.Id, err)
			}
		}
		return nil
	})
	return err
}

func (s *Syncer) findEventId(ctx context.Context, calendarID, iCalID string) (string, error) {
	var id string
	err := s.Service.Events.List(calendarID).ICalUID(iCalID).Pages(ctx, func(e *calendar.Events) error {
		for _, e := range e.Items {
			if e.RecurringEventId != "" {
				continue
			}
			id = e.Id
		}
		return nil
	})
	return id, err
}

func (s *Syncer) deleteEvent(ctx context.Context, calendarID, iCalID string) error {
	return s.Service.Events.List(calendarID).ICalUID(iCalID).Pages(ctx, func(events *calendar.Events) error {
		for _, e := range events.Items {
			err := s.Service.Events.Delete(calendarID, e.Id).Context(ctx).Do()
			if err != nil {
				return fmt.Errorf("deleting event %q", e.Id)
			}
		}
		return nil
	})
}

type SyncConfig struct {
	ID string `json:"id"`

	SourceCalendarID string `json:"source_calendar_id"`
	TargetCalendarID string `json:"target_calendar_id"`

	BusyOnly bool   `json:"busy_only,omitempty"`
	Mask     string `json:"mask,omitempty"`
}

func (c SyncConfig) ShouldSync(e *calendar.Event) bool {
	return !c.BusyOnly || e.Transparency == "opaque" || e.Transparency == ""
}

func (c SyncConfig) ICalUID(id string) string {
	return fmt.Sprintf("%s@%s", id, c.ID)
}

func (c SyncConfig) ConvertEvent(s *calendar.Event) *calendar.Event {
	e := &calendar.Event{
		ICalUID:      c.ICalUID(s.Id),
		Start:        s.Start,
		End:          s.End,
		Recurrence:   s.Recurrence,
		Transparency: s.Transparency,
	}

	if c.Mask == "" {
		e.Summary = s.Summary
		e.Description = s.Description
		e.Location = s.Location
	} else {
		e.Summary = c.Mask
	}

	return e
}

type SyncStatus struct {
	LastModified string `json:"last_modified"`
}
