// +build windows
package win_eventlog

import (
	"bytes"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
	"golang.org/x/sys/windows"
)

const renderBufferSize = 1 << 14

var sampleConfig = `
  ## Name of eventlog
  eventlog_name = "Application"
`

type WinEventLog struct {
	EventlogName string `toml:"eventlog_name"`
	Query        string `toml:"xpath_query"`
	subscription EvtHandle
	bookmark     EvtHandle
	buf          []byte
	out          *bytes.Buffer
	Log          telegraf.Logger
}

var description = "Input plugin to collect Windows eventlog messages"

func (w *WinEventLog) Description() string {
	return description
}

func (w *WinEventLog) SampleConfig() string {
	return sampleConfig
}

func (w *WinEventLog) Gather(acc telegraf.Accumulator) error {
	signalEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		w.Log.Error(err.Error())
	}
	defer windows.CloseHandle(signalEvent)
	w.Log.Debug("signalEvent:", signalEvent)

	// Initialize bookmark
	if w.bookmark == 0 {
		w.updateBookmark(0)
		w.Log.Debug("w.bookmarkonce:", w.bookmark)
	}
	w.Log.Debug("w.bookmark:", w.bookmark)

	if w.subscription == 0 {
		w.subscription, err = Subscribe(0, signalEvent, w.EventlogName, w.Query, w.bookmark, EvtSubscribeStartAfterBookmark)
		if err != nil {
			w.Log.Error("Subscribing:", err.Error(), w.bookmark)
		}
		w.Log.Debug("w.subscriptiononce:", w.bookmark)
	}
	w.Log.Debug("w.subscription:", w.subscription)

	var eventHandle EvtHandle
	for {
		eventHandles, err := EventHandles(w.subscription, 5)
		if err != nil {
			switch {
			case err == ERROR_NO_MORE_ITEMS:
				return nil
			case err != nil:
				w.Log.Error("Getting handles:", err.Error())
				return err
			}
		}

		for i := range eventHandles {
			eventHandle = eventHandles[i]
			w.out.Reset()
			err := RenderEventXML(eventHandle, w.buf, w.out)
			if err != nil {
				w.Log.Error("Rendering event:", err.Error())
			}

			evt, _ := UnmarshalEventXML(w.out.Bytes())

			w.Log.Debug("MessageRaw:", w.out.String())

			// Transform EventData to []string
			var message []string
			for _, kv := range evt.EventData.Pairs {
				message = append(message, kv.Value)
			}

			// Pass collected metrics
			acc.AddFields("win_event",
				map[string]interface{}{
					"recordID": evt.RecordID,
					"eventID":  evt.EventIdentifier.ID,
					"message":  strings.Join(message, "\n"),
					"source":   evt.Provider,
					"created":  evt.TimeCreated.SystemTime.String(),
				}, map[string]string{
					"level": evt.Level,
				})
		}
	}

	w.updateBookmark(eventHandle)
	return nil
}

func (w *WinEventLog) updateBookmark(evt EvtHandle) {
	if w.bookmark == 0 {
		lastEventsHandle, err := EvtQuery(0, w.EventlogName, w.Query, EvtQueryChannelPath|EvtQueryReverseDirection)

		lastEventHandle, err := EventHandles(lastEventsHandle, 1)
		if err != nil {
			w.Log.Error(err.Error())
		}

		w.bookmark, err = CreateBookmarkFromEvent(lastEventHandle[0])
		if err != nil {
			w.Log.Error("Setting bookmark:", err.Error())
		}
	} else {
		var err error
		w.bookmark, err = CreateBookmarkFromEvent(evt)
		if err != nil {
			w.Log.Error("Setting bookmark:", err.Error())
		}
	}
}

func init() {
	inputs.Add("win_eventlog", func() telegraf.Input {
		return &WinEventLog{
			buf: make([]byte, renderBufferSize),
			out: new(bytes.Buffer),
		}
	})
}
