package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/nuxt-apps/couchfusion/internal/app"
)

func (h apiHandler) jobEvents(writer http.ResponseWriter, request *http.Request) {
	jobID, ok := pathID(writer, request, "jobId")
	if !ok {
		return
	}
	after, ok := lastEventID(writer, request)
	if !ok {
		return
	}
	events, unsubscribe, err := h.application.SubscribeJob(jobID, after)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	defer unsubscribe()
	serveSSE(writer, request, "job", func(send func(uint64, any) bool) {
		for {
			select {
			case <-request.Context().Done():
				return
			case event, open := <-events:
				if !open || !send(event.ID, event) {
					return
				}
			}
		}
	})
}

func (h apiHandler) processEvents(writer http.ResponseWriter, request *http.Request) {
	processID, ok := pathID(writer, request, "processId")
	if !ok {
		return
	}
	after, ok := lastEventID(writer, request)
	if !ok {
		return
	}
	events, unsubscribe, err := h.application.SubscribeProcess(processID, after)
	if err != nil {
		writeApplicationError(writer, err)
		return
	}
	defer unsubscribe()
	serveSSE(writer, request, "process", func(send func(uint64, any) bool) {
		for {
			select {
			case <-request.Context().Done():
				return
			case event, open := <-events:
				if !open || !send(event.ID, event) {
					return
				}
			}
		}
	})
}

func lastEventID(writer http.ResponseWriter, request *http.Request) (uint64, bool) {
	value := request.Header.Get("Last-Event-ID")
	if value == "" {
		value = request.URL.Query().Get("after")
	}
	if value == "" {
		return 0, true
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		writeApplicationError(writer, app.ValidationError("lastEventId", "Last event ID is invalid"))
		return 0, false
	}
	return parsed, true
}

func serveSSE(writer http.ResponseWriter, request *http.Request, eventName string, stream func(func(uint64, any) bool)) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeError(writer, http.StatusInternalServerError, "internal", "Streaming is unavailable")
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Connection", "keep-alive")
	writer.Header().Set("X-Accel-Buffering", "no")
	writer.WriteHeader(http.StatusOK)
	flusher.Flush()
	stream(func(id uint64, event any) bool {
		data, err := json.Marshal(event)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(writer, "id: %d\nevent: %s\ndata: %s\n\n", id, eventName, data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	})
}
