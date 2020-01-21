// +build windows
package win_eventlog

import (
	"bytes"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
	winlogsys "github.com/influxdata/telegraf/plugins/inputs/win_eventlog/sys"
	"github.com/influxdata/telegraf/plugins/inputs/win_eventlog/sys/wineventlog"
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
	subscription wineventlog.EvtHandle
	bookmark     wineventlog.EvtHandle
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
		w.subscription, err = wineventlog.Subscribe(0, signalEvent, w.EventlogName, w.Query, w.bookmark, wineventlog.EvtSubscribeStartAfterBookmark)
		if err != nil {
			w.Log.Error("Subscribing:", err.Error(), w.bookmark)
		}
		w.Log.Debug("w.subscriptiononce:", w.bookmark)
	}
	w.Log.Debug("w.subscription:", w.subscription)

	var eventHandle wineventlog.EvtHandle
	for {
		eventHandles, err := wineventlog.EventHandles(w.subscription, 5)
		if err != nil {
			switch {
			case err == wineventlog.ERROR_NO_MORE_ITEMS:
				return nil
			case err != nil:
				w.Log.Error("Getting handles:", err.Error())
				return err
			}
		}

		for i := range eventHandles {
			eventHandle = eventHandles[i]
			w.out.Reset()
			err := wineventlog.RenderEventXML(eventHandle, w.buf, w.out)
			if err != nil {
				w.Log.Error("Rendering event:", err.Error())
			}

			evt, _ := winlogsys.UnmarshalEventXML(w.out.Bytes())

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

func (w *WinEventLog) updateBookmark(evt wineventlog.EvtHandle) {
	if w.bookmark == 0 {
		lastEventsHandle, err := wineventlog.EvtQuery(0, w.EventlogName, w.Query, wineventlog.EvtQueryChannelPath|wineventlog.EvtQueryReverseDirection)

		lastEventHandle, err := wineventlog.EventHandles(lastEventsHandle, 1)
		if err != nil {
			w.Log.Error(err.Error())
		}

		w.bookmark, err = wineventlog.CreateBookmarkFromEvent(lastEventHandle[0])
		if err != nil {
			w.Log.Error("Setting bookmark:", err.Error())
		}
	} else {
		var err error
		w.bookmark, err = wineventlog.CreateBookmarkFromEvent(evt)
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
