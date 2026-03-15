package anymodel

import (
	"bufio"
	"io"
	"strings"
)

// SSEEvent is a parsed server-sent event.
type SSEEvent struct {
	Event string
	Data  string
}

// ParseSSE reads SSE events from a reader and sends them to the returned channel.
func ParseSSE(r io.Reader) <-chan SSEEvent {
	ch := make(chan SSEEvent, 16)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(r)
		var event SSEEvent
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if event.Data != "" {
					ch <- event
				}
				event = SSEEvent{}
				continue
			}
			if strings.HasPrefix(line, "event: ") {
				event.Event = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				if event.Data != "" {
					event.Data += "\n" + data
				} else {
					event.Data = data
				}
			}
		}
		if event.Data != "" {
			ch <- event
		}
	}()
	return ch
}
