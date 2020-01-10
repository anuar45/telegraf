// +build windows
package win_eventlog

import (
	"bytes"

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
	query        string `toml:"xpath_query"`
	bookmark     wineventlog.EvtHandle
	buf          []byte
	out          *bytes.Buffer
	Log          telegraf.Logger
	signal       windows.Handle
}

var description = "Input plugin to collect Windows eventlog messages"

func (w *WinEventLog) Description() string {
	return description
}

func (w *WinEventLog) SampleConfig() string {
	return sampleConfig
}

func (w *WinEventLog) Gather(acc telegraf.Accumulator) error {
	var lastRecID uint64

	signalEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		w.Log.Error(err.Error())
	}
	defer windows.CloseHandle(signalEvent)

	if lastRecID == 0 {
		lastRecID = w.getLastEventRecID()
		//w.Log.Info("Last event RecID found:", lastRecID)
	}

	w.bookmark, err = wineventlog.CreateBookmarkFromRecordID(w.EventlogName, lastRecID)
	//w.Log.Info("Setting bookmark:", w.EventlogName, lastRecID)
	if err != nil {
		w.Log.Error("Setting bookmark:", err.Error(), w.EventlogName, lastRecID)
	}

	eventSubs, err := wineventlog.Subscribe(0, signalEvent, w.EventlogName, w.query, w.bookmark, wineventlog.EvtSubscribeStartAfterBookmark)
	if err != nil {
		w.Log.Error("Subscribing:", err.Error(), w.bookmark)
	}

	for {
		eventHandles, err := wineventlog.EventHandles(eventSubs, 5)
		if err != nil {
			w.Log.Error("Getting handles:", err.Error())
		}

		if err == wineventlog.ERROR_NO_MORE_ITEMS {
			break
		}

		for _, eventRaw := range eventHandles {
			w.out.Reset()
			err := wineventlog.RenderEventXML(eventRaw, w.buf, w.out)
			if err != nil {
				w.Log.Error("Rendering event:", err.Error())
			}

			evt, _ := winlogsys.UnmarshalEventXML(w.out.Bytes())

			acc.AddFields("event",
				map[string]interface{}{
					"recordID": evt.RecordID,
					"message":  evt.Message,
				}, map[string]string{
					"level": evt.Level,
				})
			lastRecID = evt.RecordID
		}
	}

	return nil
}

func (w *WinEventLog) getLastEventRecID() uint64 {

	var lastEventRecID uint64
	lastEventsHandle, err := wineventlog.EvtQuery(0, w.EventlogName, w.query, wineventlog.EvtQueryChannelPath|wineventlog.EvtQueryReverseDirection)

	lastEventHandle, err := wineventlog.EventHandles(lastEventsHandle, 1)
	if err != nil {
		w.Log.Error(err.Error())
	}

	err = wineventlog.RenderEventXML(lastEventHandle[0], w.buf, w.out)
	if err != nil {
		w.Log.Error(err.Error())
	}

	lastEvent, _ := winlogsys.UnmarshalEventXML(w.out.Bytes())
	if err != nil {
		w.Log.Error(err.Error())
	}

	lastEventRecID = lastEvent.RecordID

	return lastEventRecID
}

func init() {
	inputs.Add("win_eventlog", func() telegraf.Input {
		return &WinEventLog{
			buf: make([]byte, renderBufferSize),
			out: new(bytes.Buffer),
		}
	})
}
