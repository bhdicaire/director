package render

import (
	"fmt"

	"github.com/colinsurprenant/director/internal/event"
)

// The lifecycle states of an event the fold still shows, or never retires. A
// retired event's state is the Verb of its Retirement instead, so the whole
// vocabulary is these plus the Verb constants: one set of words for `director
// show` and the JSON projections alike.
const (
	StateActive           = "active"            // decision still in force, promote-marker included
	StateOpen             = "open"              // member of the open-set
	StateResumable        = "resumable"         // member of its workstream's resume stack
	StateRecorded         = "recorded"          // note: the fold never retires one
	StateResolutionMarker = "resolution-marker" // a close-marker: in no set, never retired
)

// Lifecycle is where one event stands in a Projection.
type Lifecycle struct {
	State      string     // a State constant for a live event, the Retirement's Verb for a retired one
	Retirement Retirement // zero (By == "") unless the event is retired
}

// LifecycleOf places ev in proj. Membership in the fold's own sets decides
// whether an event is live; for an event outside them the label is a lookup of
// proj.Retired, never a guess, so the fold stays the single home of the
// removal rules. An event that is neither live nor retired is an invariant
// violation (a fold rule that removes without recording why, an event the
// projection was not built from) and comes back as an error rather than as a
// default label.
func LifecycleOf(proj Projection, ev event.Event) (Lifecycle, error) {
	live := ""
	switch ev.Type {
	case event.KindDecision:
		if containsEvent(proj.Decisions, ev.ID) {
			live = StateActive
		}
	case event.KindOpenItem:
		switch {
		case isCloseMarker(ev):
			live = StateResolutionMarker
		case containsEvent(proj.OpenItems, ev.ID):
			live = StateOpen
		}
	case event.KindHandoff:
		// The event's own workstream only: another workstream's stack
		// holding the same id says nothing about this record.
		if containsEvent(proj.ResumeHandoffs[ev.Workstream], ev.ID) {
			live = StateResumable
		}
	case event.KindNote:
		if containsEvent(proj.Notes, ev.ID) {
			live = StateRecorded
		}
	default:
		return Lifecycle{}, fmt.Errorf("render: event %s has type %q, which the fold does not project", ev.ID, ev.Type)
	}
	if live != "" {
		return Lifecycle{State: live}, nil
	}
	if r, ok := proj.Retired[ev.ID]; ok {
		return Lifecycle{State: r.Verb, Retirement: r}, nil
	}
	return Lifecycle{}, fmt.Errorf("render: %s %s is in none of the projection's live sets and has no retirement entry", ev.Type, ev.ID)
}

func containsEvent(events []event.Event, id string) bool {
	for _, ev := range events {
		if ev.ID == id {
			return true
		}
	}
	return false
}
