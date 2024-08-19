package eventRouting

import (
	"fmt"
	"os"
	"strings"
    	"net/http"
    	"encoding/json"
	"bytes"

	"github.com/cloudfoundry-community/firehose-to-syslog/caching"
	fevents "github.com/cloudfoundry-community/firehose-to-syslog/events"
	"github.com/cloudfoundry-community/firehose-to-syslog/extrafields"
	"github.com/cloudfoundry-community/firehose-to-syslog/logging"
	"github.com/cloudfoundry-community/firehose-to-syslog/stats"
	"github.com/cloudfoundry/sonde-go/events"
)

type EventRoutingDefault struct {
	CachingClient  caching.Caching
	selectedEvents map[string]bool
	Stats          *stats.Stats
	ExtraFields    map[string]string
	eventFilters   []EventFilter
	postURL		string
}

func NewEventRouting(caching caching.Caching, stats *stats.Stats, filters []EventFilter, postServer string) EventRouting {
	return &EventRoutingDefault{
		CachingClient:  caching,
		selectedEvents: make(map[string]bool),
		Stats:          stats,
		ExtraFields:    make(map[string]string),
		eventFilters:   filters,
                postURL:	postServer,
	}
}

func (e *EventRoutingDefault) GetSelectedEvents() map[string]bool {
	return e.selectedEvents
}

func (e *EventRoutingDefault) RouteEvent(msg *events.Envelope) {

	var event *fevents.Event
	switch msg.GetEventType() {
	case events.Envelope_HttpStartStop:
		event = fevents.HttpStartStop(msg)
		e.Stats.Inc(stats.ConsumeHttpStartStop)
	case events.Envelope_LogMessage:
		event = fevents.LogMessage(msg)
		e.Stats.Inc(stats.ConsumeLogMessage)
	case events.Envelope_ValueMetric:
		event = fevents.ValueMetric(msg)
		e.Stats.Inc(stats.ConsumeValueMetric)
	case events.Envelope_CounterEvent:
		event = fevents.CounterEvent(msg)
		e.Stats.Inc(stats.ConsumeCounterEvent)
	case events.Envelope_Error:
		event = fevents.ErrorEvent(msg)
		e.Stats.Inc(stats.ConsumeError)
	case events.Envelope_ContainerMetric:
		event = fevents.ContainerMetric(msg)
		e.Stats.Inc(stats.ConsumeContainerMetric)
	}

	event.AnnotateWithEnveloppeData(msg)

	event.AnnotateWithMetaData(e.ExtraFields)
	if _, hasAppId := event.Fields["cf_app_id"]; hasAppId {
		event.AnnotateWithAppData(e.CachingClient)
		//We do not ship Event for now event only concern app type of stream
		for _, filter := range e.eventFilters {
			if filter(event) {
				e.Stats.Inc(stats.Ignored)
				return
			}
		}
	}

	values := make(map[string]string)
	for k, v := range event.Fields {
		values[k] = fmt.Sprintf("%s",v)
	}
	values["message"]=event.Msg

    	json_data, err := json.Marshal(values)

	http.Post(e.postURL, "application/json", bytes.NewBuffer(json_data))

    	if err != nil {
        	logging.LogStd(fmt.Sprintf("%s",err), true)
    	}

	e.Stats.Inc(stats.Publish)

}

func (e *EventRoutingDefault) SetupEventRouting(wantedEvents string) error {
	e.selectedEvents = make(map[string]bool)
	if wantedEvents == "" {
		e.selectedEvents["LogMessage"] = true
	} else {
		for _, event := range strings.Split(wantedEvents, ",") {
			if IsAuthorizedEvent(strings.TrimSpace(event)) {
				e.selectedEvents[strings.TrimSpace(event)] = true
				logging.LogStd(fmt.Sprintf("Event Type [%s] is included in the fireshose!", event), false)
			} else {
				return fmt.Errorf("Rejected Event Name [%s] - Valid events: %s", event, GetListAuthorizedEventEvents())
			}
		}
	}
	return nil
}

func (e *EventRoutingDefault) SetPostUrl(postURL string) {
	e.postURL = postURL
}

func (e *EventRoutingDefault) SetExtraFields(extraEventsString string) {
	// Parse extra fields from cmd call
	extraFields, err := extrafields.ParseExtraFields(extraEventsString)
	if err != nil {
		logging.LogError("Error parsing extra fields: ", err)
		os.Exit(1)
	}
	e.ExtraFields = extraFields
}
