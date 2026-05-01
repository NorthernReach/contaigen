package progress

import "context"

type State string

const (
	StateActive State = "active"
	StateDone   State = "done"
	StateInfo   State = "info"
)

type Event struct {
	State   State
	Message string
	Detail  string
	Current int64
	Total   int64
}

type Reporter interface {
	ReportProgress(Event)
}

type reporterKey struct{}

// Progress is carried on context so engine/runtime packages can emit useful
// status without importing CLI rendering code.
func WithReporter(ctx context.Context, reporter Reporter) context.Context {
	if reporter == nil {
		return ctx
	}
	return context.WithValue(ctx, reporterKey{}, reporter)
}

func FromContext(ctx context.Context) Reporter {
	reporter, _ := ctx.Value(reporterKey{}).(Reporter)
	return reporter
}

func Report(ctx context.Context, event Event) {
	reporter := FromContext(ctx)
	if reporter == nil {
		return
	}
	reporter.ReportProgress(event)
}

func Active(ctx context.Context, message string, detail string) {
	Report(ctx, Event{State: StateActive, Message: message, Detail: detail})
}

func Done(ctx context.Context, message string, detail string) {
	Report(ctx, Event{State: StateDone, Message: message, Detail: detail})
}

func Info(ctx context.Context, message string, detail string) {
	Report(ctx, Event{State: StateInfo, Message: message, Detail: detail})
}
