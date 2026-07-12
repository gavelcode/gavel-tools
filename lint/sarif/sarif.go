// Package sarif holds the shared SARIF invocation types every Gavel lint
// wrapper uses to report whether its analyzer ran to completion and, when it did
// not, the concrete reason — so a consumer (gavel judge) can both fail the
// verdict and tell the user which tool broke and why.
package sarif

// Invocation is the SARIF run.invocations[] entry. executionSuccessful is a
// required SARIF property: true only when the tool ran completely. A partial or
// failed run sets it false and explains itself in toolExecutionNotifications.
type Invocation struct {
	ExecutionSuccessful        bool           `json:"executionSuccessful"`
	ToolExecutionNotifications []Notification `json:"toolExecutionNotifications,omitempty"`
}

// Notification is one runtime condition the tool reported during analysis.
type Notification struct {
	Level   string  `json:"level"`
	Message Message `json:"message"`
}

// Message is the SARIF message object (text only — all we need here).
type Message struct {
	Text string `json:"text"`
}

// Successful reports a complete, trustworthy analysis run.
func Successful() Invocation {
	return Invocation{ExecutionSuccessful: true}
}

// Failed reports that the analyzer could not complete. Each note must carry the
// concrete reason (the tool's real error), not a generic "failed", so the
// consumer has enough to fix the environment; every note becomes an error-level
// toolExecutionNotification.
func Failed(notes ...string) Invocation {
	return withNotifications(false, "error", notes)
}

// Degraded reports a run that completed but could only analyze part of the
// target — the tool ran, so executionSuccessful stays true, but each note
// records why coverage was incomplete as a warning-level notification. The
// consumer surfaces this honestly without failing the verdict, distinguishing
// "I could not fully analyze this" from "I could not run".
func Degraded(notes ...string) Invocation {
	return withNotifications(true, "warning", notes)
}

func withNotifications(successful bool, level string, notes []string) Invocation {
	invocation := Invocation{ExecutionSuccessful: successful}
	for _, note := range notes {
		invocation.ToolExecutionNotifications = append(invocation.ToolExecutionNotifications, Notification{
			Level:   level,
			Message: Message{Text: note},
		})
	}
	return invocation
}
