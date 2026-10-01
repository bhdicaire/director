package render

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/colinsurprenant/director/internal/event"
)

func TestProjectionJSONDeterministicAndComplete(t *testing.T) {
	events, ids := richSet(t)
	longBody := strings.Repeat("complete rationale; ", 80)
	for i := range events {
		if events[i].ID == ids.decB {
			events[i].Body = longBody
		}
	}

	want, err := ProjectionJSON(Fold(events), "widget")
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []int64{1, 7, 99, 2026} {
		got, err := ProjectionJSON(Fold(shuffled(events, seed)), "widget")
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("JSON projection changed under input shuffle seed %d:\n--- want ---\n%s\n--- got ---\n%s", seed, want, got)
		}
	}

	var got JSONProjection
	if err := json.Unmarshal(want, &got); err != nil {
		t.Fatalf("parse JSON projection: %v\n%s", err, want)
	}
	if got.SchemaVersion != JSONSchemaVersion || got.Project != "widget" {
		t.Errorf("envelope = version %d project %q, want version %d project widget", got.SchemaVersion, got.Project, JSONSchemaVersion)
	}
	if !reflect.DeepEqual(workstreamNames(got.ResumeHandoffs), []string{"ws1", "ws2"}) {
		t.Errorf("resume workstreams = %v, want [ws1 ws2]", workstreamNames(got.ResumeHandoffs))
	}
	for _, decision := range got.Decisions {
		if decision.Event.ID == ids.decB {
			if decision.Lifecycle != "active" {
				t.Errorf("active decision lifecycle = %q", decision.Lifecycle)
			}
			if decision.Event.Body != longBody {
				t.Error("JSON projection truncated or changed the complete decision body")
			}
		}
	}
	if !strings.HasSuffix(string(want), "\n") {
		t.Error("JSON projection must end with a newline")
	}

	empty, err := ProjectionJSON(Fold(nil), "empty")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"decisions": []`, `"open_items": []`, `"resume_handoffs": []`} {
		if !strings.Contains(string(empty), field) {
			t.Errorf("empty JSON projection missing %s:\n%s", field, empty)
		}
	}
}

func workstreamNames(stacks []ResumeHandoffState) []string {
	out := make([]string, 0, len(stacks))
	for _, stack := range stacks {
		out = append(out, stack.Workstream)
	}
	return out
}

func TestShowJSONLifecycle(t *testing.T) {
	promoted := mint(t)
	promoteMarker := mint(t)
	superseded := mint(t)
	successor := mint(t)
	closed := mint(t)
	closeMarker := mint(t)
	open := mint(t)
	implicitlySuperseded := mint(t)
	resumable := mint(t)
	concluded := mint(t)
	concludingNote := mint(t)
	explicitlySuperseded := mint(t)
	explicitSuccessor := mint(t)

	events := []event.Event{
		{ID: promoted, SchemaVersion: event.SchemaVersion, Type: event.KindDecision, Workstream: "ws", Body: "promoted decision"},
		{ID: promoteMarker, SchemaVersion: event.SchemaVersion, Type: event.KindDecision, Status: event.StatusPromoted, Workstream: "ws", Refs: []string{promoted}, PromotedTo: "docs/decision.md", Body: "promotion marker"},
		{ID: superseded, SchemaVersion: event.SchemaVersion, Type: event.KindDecision, Workstream: "ws", Body: "old decision"},
		{ID: successor, SchemaVersion: event.SchemaVersion, Type: event.KindDecision, Workstream: "ws", Refs: []string{superseded}, Body: "new decision"},
		{ID: closed, SchemaVersion: event.SchemaVersion, Type: event.KindOpenItem, Status: event.StatusOpen, Workstream: "ws", Body: "closed item"},
		{ID: closeMarker, SchemaVersion: event.SchemaVersion, Type: event.KindOpenItem, Status: event.StatusClosed, Workstream: "ws", Refs: []string{closed}, Body: "resolution"},
		{ID: open, SchemaVersion: event.SchemaVersion, Type: event.KindOpenItem, Status: event.StatusOpen, Workstream: "ws", Body: "open item"},
		{ID: implicitlySuperseded, SchemaVersion: event.SchemaVersion, Type: event.KindHandoff, Workstream: "ws", Body: "old position"},
		{ID: resumable, SchemaVersion: event.SchemaVersion, Type: event.KindHandoff, Workstream: "ws", Body: "current position"},
		{ID: concluded, SchemaVersion: event.SchemaVersion, Type: event.KindHandoff, Workstream: "ws2", Body: "completed position"},
		{ID: concludingNote, SchemaVersion: event.SchemaVersion, Type: event.KindNote, Workstream: "ws2", Refs: []string{concluded}, Body: "completed"},
		{ID: explicitlySuperseded, SchemaVersion: event.SchemaVersion, Type: event.KindHandoff, Workstream: "ws3", Body: "position A"},
		{ID: explicitSuccessor, SchemaVersion: event.SchemaVersion, Type: event.KindHandoff, Workstream: "ws3", Refs: []string{explicitlySuperseded}, Body: "position B"},
	}
	proj := Fold(events)

	wants := map[string]string{
		promoted:             "promoted",
		promoteMarker:        "active",
		superseded:           "superseded",
		successor:            "active",
		closed:               "closed",
		closeMarker:          "resolution-marker",
		open:                 "open",
		implicitlySuperseded: "superseded",
		resumable:            "resumable",
		concluded:            "concluded",
		concludingNote:       "recorded",
		explicitlySuperseded: "superseded",
		explicitSuccessor:    "resumable",
	}
	for _, target := range events {
		data, err := ShowJSON(proj, "widget", target)
		if err != nil {
			t.Fatalf("show %s: %v", target.ID, err)
		}
		var got JSONEvent
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("parse show %s: %v", target.ID, err)
		}
		if got.Record.Lifecycle != wants[target.ID] {
			t.Errorf("event %s lifecycle = %q, want %q", target.ID, got.Record.Lifecycle, wants[target.ID])
		}
		if !reflect.DeepEqual(got.Record.Event, target) {
			t.Errorf("event %s record changed during JSON serialization", target.ID)
		}
	}
}

// logSpec is one event of a precedence fixture, named by label so a test can
// mint the ids in whatever order it needs and still say who refs whom.
type logSpec struct {
	typ  event.Kind
	ws   string
	refs []string // labels
	mark bool     // a promote-marker (decisions only)
}

// buildLog mints ids in the order given and returns the events, in that same
// (ULID-ascending) order, with refs resolved to ids.
func buildLog(t *testing.T, order []string, specs map[string]logSpec) ([]event.Event, map[string]string) {
	t.Helper()
	ids := make(map[string]string, len(order))
	for _, label := range order {
		ids[label] = mint(t)
	}
	events := make([]event.Event, 0, len(order))
	for _, label := range order {
		spec := specs[label]
		ev := event.Event{ID: ids[label], SchemaVersion: event.SchemaVersion, Type: spec.typ, Workstream: spec.ws, Body: label}
		for _, ref := range spec.refs {
			ev.Refs = append(ev.Refs, ids[ref])
		}
		if spec.mark {
			ev.Status = event.StatusPromoted
			ev.PromotedTo = "docs/why.md"
		}
		events = append(events, ev)
	}
	return events, ids
}

// A handoff the fold retires by two paths gets the lifecycle of the LOWEST-ULID
// retirer, whichever path that is: the verb comes from proj.Retired, never from
// a precedence rule of the JSON layer's own.
func TestShowJSONLifecycleLowestRetirerStands(t *testing.T) {
	// direct: the note names h1 itself. swept: the note names hx, a position
	// above h1, so it reaches h1 only through the conclusion high-water mark
	// (h0 gives hx an explicit ref, keeping h1 off the implicit mark's path).
	direct := map[string]logSpec{
		"h1":   {typ: event.KindHandoff, ws: "w"},
		"h2":   {typ: event.KindHandoff, ws: "w", refs: []string{"h1"}},
		"note": {typ: event.KindNote, ws: "w", refs: []string{"h1"}},
	}
	swept := map[string]logSpec{
		"h0":   {typ: event.KindHandoff, ws: "w"},
		"h1":   {typ: event.KindHandoff, ws: "w"},
		"hx":   {typ: event.KindHandoff, ws: "w", refs: []string{"h0"}},
		"h2":   {typ: event.KindHandoff, ws: "w", refs: []string{"h1"}},
		"note": {typ: event.KindNote, ws: "w", refs: []string{"hx"}},
	}
	tests := []struct {
		name   string
		specs  map[string]logSpec
		order  []string
		want   string
		wantBy string
	}{
		{"direct, conclusion first", direct, []string{"h1", "note", "h2"}, VerbConcluded, "note"},
		{"direct, supersession first", direct, []string{"h1", "h2", "note"}, VerbSuperseded, "h2"},
		{"swept by the mark, conclusion first", swept, []string{"h0", "h1", "hx", "note", "h2"}, VerbConcluded, "note"},
		{"swept by the mark, supersession first", swept, []string{"h0", "h1", "hx", "h2", "note"}, VerbSuperseded, "h2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, ids := buildLog(t, tt.order, tt.specs)
			proj := Fold(events)
			r := proj.Retired[ids["h1"]]
			if r.Verb != tt.want || r.By != ids[tt.wantBy] {
				t.Fatalf("fold retired h1 as %+v, want %s by %s", r, tt.want, tt.wantBy)
			}
			for _, ev := range events {
				if ev.ID != ids["h1"] {
					continue
				}
				data, err := ShowJSON(proj, "widget", ev)
				if err != nil {
					t.Fatal(err)
				}
				var got JSONEvent
				if err := json.Unmarshal(data, &got); err != nil {
					t.Fatal(err)
				}
				if got.Record.Lifecycle != tt.want {
					t.Errorf("lifecycle = %q, want %q", got.Record.Lifecycle, tt.want)
				}
			}
		})
	}
}

// The same rule for a decision that is both promoted and superseded: the
// lowest-ULID of the promote-marker and the superseding decision stands.
func TestShowJSONLifecycleDecisionPromotedAndSuperseded(t *testing.T) {
	specs := map[string]logSpec{
		"d": {typ: event.KindDecision, ws: "w"},
		"s": {typ: event.KindDecision, ws: "w", refs: []string{"d"}},
		"p": {typ: event.KindDecision, ws: "w", refs: []string{"d"}, mark: true},
	}
	tests := []struct {
		name  string
		order []string
		want  string
	}{
		{"promote-marker first", []string{"d", "p", "s"}, VerbPromoted},
		{"superseding decision first", []string{"d", "s", "p"}, VerbSuperseded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, ids := buildLog(t, tt.order, specs)
			lc, err := LifecycleOf(Fold(events), events[0])
			if err != nil {
				t.Fatal(err)
			}
			if events[0].ID != ids["d"] || lc.State != tt.want {
				t.Errorf("decision lifecycle = %q, want %q", lc.State, tt.want)
			}
		})
	}
}

// A handoff's resumable check reads its own workstream's stack only. This log
// reuses an id across workstreams (the store refuses that since #69, but the
// fold still folds a log that has one): w1's position is retired by w1's later
// handoff, and w2's same-id handoff sitting on w2's stack must not revive it.
func TestShowJSONLifecycleResumableIsScopedToItsWorkstream(t *testing.T) {
	reused, later := mint(t), mint(t)
	events := []event.Event{
		{ID: reused, SchemaVersion: event.SchemaVersion, Type: event.KindHandoff, Workstream: "w1", Body: "w1 position"},
		{ID: reused, SchemaVersion: event.SchemaVersion, Type: event.KindHandoff, Workstream: "w2", Body: "w2 position"},
		{ID: later, SchemaVersion: event.SchemaVersion, Type: event.KindHandoff, Workstream: "w1", Body: "w1 newer position"},
	}
	proj := Fold(events)
	if !containsEvent(proj.ResumeHandoffs["w2"], reused) {
		t.Fatalf("fixture: w2's position should be resumable, stacks = %v", proj.ResumeHandoffs)
	}
	for _, tt := range []struct {
		ev   event.Event
		want string
	}{
		{events[0], VerbSuperseded},
		{events[2], StateResumable},
	} {
		lc, err := LifecycleOf(proj, tt.ev)
		if err != nil {
			t.Fatal(err)
		}
		if lc.State != tt.want {
			t.Errorf("%s handoff %s lifecycle = %q, want %q", tt.ev.Workstream, tt.ev.ID, lc.State, tt.want)
		}
	}
}

// A lifecycle the fold cannot account for is an error, never a default label:
// an event the projection was not built from, and a type the fold ignores.
func TestLifecycleOfRejectsWhatTheFoldCannotPlace(t *testing.T) {
	stranger := event.Event{ID: mint(t), SchemaVersion: event.SchemaVersion, Type: event.KindDecision, Workstream: "w", Body: "not in the log"}
	foreign := event.Event{ID: mint(t), SchemaVersion: event.SchemaVersion, Type: event.Kind("blocker"), Workstream: "w", Body: "no such kind"}
	proj := Fold([]event.Event{{ID: mint(t), SchemaVersion: event.SchemaVersion, Type: event.KindDecision, Workstream: "w"}, foreign})

	for _, ev := range []event.Event{stranger, foreign} {
		lc, err := LifecycleOf(proj, ev)
		if err == nil {
			t.Errorf("%s %s lifecycle = %+v, want an error", ev.Type, ev.ID, lc)
		}
		if got, want := errors.Is(err, ErrUnprojectedKind), ev.Type == foreign.Type; got != want {
			t.Errorf("%s %s: errors.Is(err, ErrUnprojectedKind) = %v, want %v (%v)", ev.Type, ev.ID, got, want, err)
		}
		if data, err := ShowJSON(proj, "widget", ev); err == nil {
			t.Errorf("ShowJSON(%s %s) = %s, want an error", ev.Type, ev.ID, data)
		}
	}
}

// Retired is keyed by id alone, so an id reused across kinds can hand an event
// another kind's verb. Here the open-item and the decision share x; a close-marker
// retires the open-item, but the lower-ULID superseding decision stands as x's
// retirer, so the open-item would read "superseded", a word only decisions and
// handoffs have. That is an error, and the decision (which is superseded)
// still resolves.
func TestLifecycleOfRejectsAVerbOutsideTheKindsVocabulary(t *testing.T) {
	x, superseder, closer := mint(t), mint(t), mint(t)
	item := event.Event{ID: x, SchemaVersion: event.SchemaVersion, Type: event.KindOpenItem, Status: event.StatusOpen, Workstream: "w", Body: "item"}
	decision := event.Event{ID: x, SchemaVersion: event.SchemaVersion, Type: event.KindDecision, Workstream: "w", Body: "decision"}
	proj := Fold([]event.Event{
		item, decision,
		{ID: superseder, SchemaVersion: event.SchemaVersion, Type: event.KindDecision, Workstream: "w", Refs: []string{x}, Body: "supersedes"},
		{ID: closer, SchemaVersion: event.SchemaVersion, Type: event.KindOpenItem, Status: event.StatusClosed, Workstream: "w", Refs: []string{x}, Body: "closed"},
	})
	if got := proj.Retired[x]; got.Verb != VerbSuperseded || got.By != superseder {
		t.Fatalf("fixture: Retired[x] = %+v, want superseded by the lower ULID", got)
	}

	if lc, err := LifecycleOf(proj, item); err == nil {
		t.Errorf("open-item lifecycle = %+v, want an error", lc)
	}
	if lc, err := LifecycleOf(proj, decision); err != nil || lc.State != VerbSuperseded {
		t.Errorf("decision lifecycle = %+v, %v, want superseded", lc, err)
	}
}

// The envelope collections are [] when empty, but the nested event is the
// durable record and keeps the event schema's optional-field rules: a ref-less
// event has no refs key at all, one with refs carries them.
func TestProjectionJSONNestedEventKeepsDurableOptionalFields(t *testing.T) {
	bare := event.Event{ID: mint(t), SchemaVersion: event.SchemaVersion, Type: event.KindDecision, Workstream: "w", Body: "no refs"}
	linked := event.Event{ID: mint(t), SchemaVersion: event.SchemaVersion, Type: event.KindDecision, Workstream: "w", Refs: []string{mint(t)}, Body: "has refs"}
	data, err := ProjectionJSON(Fold([]event.Event{bare, linked}), "widget")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Decisions []struct {
			Event map[string]any `json:"event"`
		} `json:"decisions"`
		OpenItems []any `json:"open_items"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Decisions) != 2 {
		t.Fatalf("decisions = %d, want 2:\n%s", len(got.Decisions), data)
	}
	if _, present := got.Decisions[0].Event["refs"]; present {
		t.Errorf("ref-less event carries a refs key; the nested event must follow the durable schema:\n%s", data)
	}
	if refs, present := got.Decisions[1].Event["refs"]; !present || len(refs.([]any)) != 1 {
		t.Errorf("event with refs lost them: %v", got.Decisions[1].Event)
	}
	if got.OpenItems == nil || len(got.OpenItems) != 0 {
		t.Errorf("empty envelope collection = %v, want []", got.OpenItems)
	}
}

// Bodies are code and prose, so < > & stay as written rather than becoming
// < > &, and a byte-level grep for a body finds it.
func TestJSONDoesNotHTMLEscape(t *testing.T) {
	body := `use <T> & "quotes" in a>b`
	ev := event.Event{ID: mint(t), SchemaVersion: event.SchemaVersion, Type: event.KindDecision, Workstream: "w", Body: body}
	proj := Fold([]event.Event{ev})

	list, err := ProjectionJSON(proj, "widget")
	if err != nil {
		t.Fatal(err)
	}
	one, err := ShowJSON(proj, "widget", ev)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"render": list, "show": one} {
		if strings.Contains(string(data), `\u003`) || strings.Contains(string(data), `\u0026`) {
			t.Errorf("%s JSON HTML-escaped the body:\n%s", name, data)
		}
		if !strings.Contains(string(data), `use <T> & \"quotes\" in a>b`) {
			t.Errorf("%s JSON does not carry the body verbatim (modulo JSON quoting):\n%s", name, data)
		}
	}
	if !strings.HasSuffix(string(one), "}\n") || !strings.Contains(string(one), "\n  \"record\": {\n") {
		t.Errorf("JSON is not two-space indented with a trailing newline:\n%s", one)
	}
}
