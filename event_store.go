package eventmaster

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	eventmaster "github.com/ContextLogic/eventmaster/proto"
	"github.com/pkg/errors"
	"github.com/satori/go.uuid"
	"github.com/segmentio/ksuid"
	"github.com/xeipuuv/gojsonschema"
)

type Event struct {
	EventID       string                 `json:"event_id"`
	ParentEventID string                 `json:"parent_event_id"`
	EventTime     int64                  `json:"event_time"`
	DcID          string                 `json:"dc_id"`
	TopicID       string                 `json:"topic_id"`
	Tags          []string               `json:"tag_set"`
	Host          string                 `json:"host"`
	TargetHosts   []string               `json:"target_host_set"`
	User          string                 `json:"user"`
	Data          map[string]interface{} `json:"data"`
	ReceivedTime  int64                  `json:"received_time"`
}

type Events []*Event

func (evts Events) Len() int {
	return len(evts)
}

func (evts Events) Less(i, j int) bool {
	return evts[i].EventTime > evts[j].EventTime
}

func (evts Events) Swap(i, j int) {
	evts[i], evts[j] = evts[j], evts[i]
}

type UnaddedEvent struct {
	ParentEventID string                 `json:"parent_event_id"`
	EventTime     int64                  `json:"event_time"`
	Dc            string                 `json:"dc"`
	TopicName     string                 `json:"topic_name"`
	Tags          []string               `json:"tag_set"`
	Host          string                 `json:"host"`
	TargetHosts   []string               `json:"target_host_set"`
	User          string                 `json:"user"`
	Data          map[string]interface{} `json:"data"`
}

var v struct{}

type RawTopic struct {
	ID     string
	Name   string
	Schema string
}

type Topic struct {
	ID     string                 `json:"topic_id"`
	Name   string                 `json:"topic_name"`
	Schema map[string]interface{} `json:"data_schema"`
}

type Dc struct {
	ID   string `json:"dc_id"`
	Name string `json:"dc_name"`
}

type EventStore struct {
	ds                       DataStore
	topicNameToId            map[string]string                   // map of name to id
	topicIdToName            map[string]string                   // map of id to name
	topicSchemaMap           map[string]*gojsonschema.Schema     // map of topic id to json loader for schema validation
	topicSchemaPropertiesMap map[string](map[string]interface{}) // map of topic id to properties of topic data
	dcNameToId               map[string]string                   // map of name to id
	dcIdToName               map[string]string                   // map of id to name
	indexNames               []string                            // list of name of all indices in es cluster
	topicMutex               *sync.RWMutex
	dcMutex                  *sync.RWMutex
	indexMutex               *sync.RWMutex
}

func NewEventStore(ds DataStore) (*EventStore, error) {
	return &EventStore{
		ds:                       ds,
		topicMutex:               &sync.RWMutex{},
		dcMutex:                  &sync.RWMutex{},
		indexMutex:               &sync.RWMutex{},
		topicNameToId:            make(map[string]string),
		topicIdToName:            make(map[string]string),
		topicSchemaMap:           make(map[string]*gojsonschema.Schema),
		topicSchemaPropertiesMap: make(map[string](map[string]interface{})),
		dcNameToId:               make(map[string]string),
		dcIdToName:               make(map[string]string),
	}, nil
}

func (es *EventStore) getTopicIds() map[string]string {
	es.topicMutex.RLock()
	ids := es.topicIdToName
	es.topicMutex.RUnlock()
	return ids
}

func (es *EventStore) getTopicId(topic string) string {
	es.topicMutex.RLock()
	id := es.topicNameToId[strings.ToLower(topic)]
	es.topicMutex.RUnlock()
	return id
}

func (es *EventStore) getTopicName(id string) string {
	es.topicMutex.RLock()
	name := es.topicIdToName[id]
	es.topicMutex.RUnlock()
	return name
}

func (es *EventStore) getTopicSchema(id string) *gojsonschema.Schema {
	es.topicMutex.RLock()
	schema := es.topicSchemaMap[id]
	es.topicMutex.RUnlock()
	return schema
}

func (es *EventStore) getTopicSchemaProperties(id string) map[string]interface{} {
	es.topicMutex.RLock()
	schema := es.topicSchemaPropertiesMap[id]
	es.topicMutex.RUnlock()
	return schema
}

func (es *EventStore) getDcId(dc string) string {
	es.dcMutex.RLock()
	id := es.dcNameToId[strings.ToLower(dc)]
	es.dcMutex.RUnlock()
	return id
}

func (es *EventStore) getDcName(id string) string {
	es.dcMutex.RLock()
	name := es.dcIdToName[id]
	es.dcMutex.RUnlock()
	return name
}

func (es *EventStore) validateSchema(schema string) (*gojsonschema.Schema, bool) {
	loader := gojsonschema.NewStringLoader(schema)
	jsonSchema, err := gojsonschema.NewSchema(loader)
	if err != nil {
		return nil, false
	}
	return jsonSchema, true
}

func (es *EventStore) insertDefaults(s map[string]interface{}, m map[string]interface{}) {
	properties := s["properties"]
	p, _ := properties.(map[string]interface{})
	insertDefaults(p, m)
}

func (es *EventStore) augmentEvent(event *UnaddedEvent) (*Event, error) {
	// validate Event
	if event.Dc == "" {
		return nil, errors.New("Event missing dc")
	} else if event.Host == "" {
		return nil, errors.New("Event missing host")
	} else if event.TopicName == "" {
		return nil, errors.New("Event missing topic_name")
	}

	if event.EventTime == 0 {
		event.EventTime = time.Now().Unix()
	}

	dcID := es.getDcId(strings.ToLower(event.Dc))
	if dcID == "" {
		return nil, errors.New(fmt.Sprintf("Dc '%s' does not exist in dc table", strings.ToLower(event.Dc)))
	}
	topicID := es.getTopicId(strings.ToLower(event.TopicName))
	if topicID == "" {
		return nil, errors.New(fmt.Sprintf("Topic '%s' does not exist in topic table", strings.ToLower(event.TopicName)))
	}
	topicSchema := es.getTopicSchema(topicID)
	data := "{}"
	if topicSchema != nil {
		if event.Data == nil {
			event.Data = make(map[string]interface{})
		}
		dataBytes, err := json.Marshal(event.Data)
		if err != nil {
			return nil, errors.Wrap(err, "Error marshalling data with defaults into json")
		}
		data = string(dataBytes)
		dataLoader := gojsonschema.NewStringLoader(data)
		result, err := topicSchema.Validate(dataLoader)
		if err != nil {
			return nil, errors.Wrap(err, "Error validating event data against schema")
		}
		if !result.Valid() {
			errMsg := ""
			for _, err := range result.Errors() {
				errMsg = fmt.Sprintf("%s, %s", errMsg, err)
			}
			return nil, errors.New(errMsg)
		}
	}

	eventID, err := ksuid.NewRandomWithTime(time.Unix(event.EventTime, 0).UTC())
	if err != nil {
		return nil, errors.Wrap(err, "Error creating event ID:")
	}

	return &Event{
		EventID:       eventID.String(),
		ParentEventID: event.ParentEventID,
		EventTime:     event.EventTime * 1000,
		DcID:          dcID,
		TopicID:       topicID,
		Tags:          event.Tags,
		Host:          event.Host,
		TargetHosts:   event.TargetHosts,
		User:          event.User,
		Data:          event.Data,
		ReceivedTime:  time.Now().Unix() * 1000,
	}, nil
}

func (es *EventStore) Find(q *eventmaster.Query) (Events, error) {
	start := time.Now()
	defer func() {
		eventStoreTimer.WithLabelValues("Find").Observe(trackTime(start))
	}()
	if q.StartEventTime == 0 || q.EndEventTime == 0 || q.EndEventTime < q.StartEventTime {
		return nil, errors.New("Must specify valid start and end event time")
	}
	var topicIds, dcIds []string
	for _, topic := range q.TopicName {
		topicIds = append(topicIds, es.getTopicId(topic))
	}
	for _, dc := range q.Dc {
		dcIds = append(dcIds, es.getDcId(dc))
	}
	evts, err := es.ds.Find(q, topicIds, dcIds)
	if err != nil {
		eventStoreDbErrCounter.WithLabelValues("cassandra", "read").Inc()
		return nil, errors.Wrap(err, "Error executing find in data source")
	}
	sort.Sort(evts)
	return evts, nil
}

func (es *EventStore) FindById(id string) (*Event, error) {
	start := time.Now()
	defer func() {
		eventStoreTimer.WithLabelValues("Find").Observe(trackTime(start))
	}()
	evt, err := es.ds.FindById(id, true)
	if err != nil {
		eventStoreDbErrCounter.WithLabelValues("cassandra", "read").Inc()
		return nil, errors.Wrap(err, "Error executing find in data source")
	}
	if evt == nil {
		return nil, errors.New("Could not find event matching id " + id)
	}
	propertiesSchema := es.getTopicSchemaProperties(evt.TopicID)
	if evt.Data == nil {
		evt.Data = make(map[string]interface{})
	}
	es.insertDefaults(propertiesSchema, evt.Data)
	return evt, nil
}

func (es *EventStore) FindIds(q *eventmaster.TimeQuery, stream streamFn) error {
	start := time.Now()
	defer func() {
		eventStoreTimer.WithLabelValues("FindIds").Observe(trackTime(start))
	}()
	if q.Limit == 0 {
		q.Limit = 200
	}
	if q.StartEventTime == 0 || q.EndEventTime == 0 || q.EndEventTime < q.StartEventTime {
		return errors.New("Start and end event time must be specified")
	}

	return es.ds.FindIds(q, stream)
}

func (es *EventStore) AddEvent(event *UnaddedEvent) (string, error) {
	start := time.Now()
	defer func() {
		eventStoreTimer.WithLabelValues("AddEvent").Observe(trackTime(start))
	}()

	evt, err := es.augmentEvent(event)
	if err != nil {
		return "", errors.Wrap(err, "Error augmenting event")
	}

	if err = es.ds.AddEvent(evt); err != nil {
		eventStoreDbErrCounter.WithLabelValues("cassandra", "write").Inc()
		return "", errors.Wrap(err, "Error executing insert query in Cassandra")
	}

	fmt.Println("Event added:", evt.EventID)
	return evt.EventID, nil
}

func (es *EventStore) GetTopics() ([]Topic, error) {
	start := time.Now()
	defer func() {
		eventStoreTimer.WithLabelValues("GetTopics").Observe(trackTime(start))
	}()
	topics, err := es.ds.GetTopics()
	if err != nil {
		eventStoreDbErrCounter.WithLabelValues("cassandra", "read").Inc()
		return nil, errors.Wrap(err, "Error getting topics from data source")
	}
	return topics, nil
}

func (es *EventStore) GetDcs() ([]Dc, error) {
	start := time.Now()
	defer func() {
		eventStoreTimer.WithLabelValues("GetDcs").Observe(trackTime(start))
	}()

	dcs, err := es.ds.GetDcs()
	if err != nil {
		eventStoreDbErrCounter.WithLabelValues("cassandra", "read").Inc()
		return nil, errors.Wrap(err, "Error deleting topic from data source")
	}
	return dcs, nil
}

func (es *EventStore) AddTopic(topic Topic) (string, error) {
	start := time.Now()
	defer func() {
		eventStoreTimer.WithLabelValues("AddTopic").Observe(trackTime(start))
	}()

	name := topic.Name
	schema := topic.Schema

	if name == "" {
		return "", errors.New("Topic name cannot be empty")
	} else if es.getTopicId(name) != "" {
		return "", errors.New("Topic with name already exists")
	}

	schemaStr := "{}"
	if schema != nil {
		schemaBytes, err := json.Marshal(schema)
		if err != nil {
			return "", errors.Wrap(err, "Error marshalling schema into json")
		}
		schemaStr = string(schemaBytes)
	}

	jsonSchema, ok := es.validateSchema(schemaStr)
	if !ok {
		return "", errors.New("Error adding topic - schema is not in valid JSON format")
	}

	id := uuid.NewV4().String()
	if err := es.ds.AddTopic(RawTopic{
		ID:     id,
		Name:   name,
		Schema: schemaStr,
	}); err != nil {
		eventStoreDbErrCounter.WithLabelValues("cassandra", "write").Inc()
		return "", errors.Wrap(err, "Error adding topic to data source")
	}

	es.topicMutex.Lock()
	es.topicNameToId[name] = id
	es.topicIdToName[id] = name
	es.topicSchemaPropertiesMap[id] = schema
	es.topicSchemaMap[id] = jsonSchema
	es.topicMutex.Unlock()

	fmt.Println("Topic Added:", name, id)
	return id, nil
}

func (es *EventStore) UpdateTopic(oldName string, td Topic) (string, error) {
	start := time.Now()
	defer func() {
		eventStoreTimer.WithLabelValues("UpdateTopic").Observe(trackTime(start))
	}()

	newName := td.Name
	schema := td.Schema

	if newName == "" {
		newName = oldName
	}

	id := es.getTopicId(newName)
	if oldName != newName && id != "" {
		return "", errors.New(fmt.Sprintf("Error updating topic - topic with name %s already exists", newName))
	}
	id = es.getTopicId(oldName)
	if id == "" {
		return "", errors.New(fmt.Sprintf("Error updating topic - topic with name %s doesn't exist", oldName))
	}

	var jsonSchema *gojsonschema.Schema
	var ok bool
	schemaStr := "{}"
	if schema != nil {
		// validate new schema and check that it's backwards compatible
		schemaBytes, err := json.Marshal(schema)
		if err != nil {
			return "", errors.Wrap(err, "Error marshalling schema into json")
		}
		schemaStr = string(schemaBytes)
		jsonSchema, ok = es.validateSchema(schemaStr)
		if !ok {
			return "", errors.New("Error adding topic - schema is not in valid JSON schema format")
		}

		old := es.getTopicSchemaProperties(id)
		ok = checkBackwardsCompatible(old, schema)
		if !ok {
			return "", errors.New("Error adding topic - new schema is not backwards compatible")
		}
	}

	if err := es.ds.UpdateTopic(RawTopic{
		ID:     id,
		Name:   newName,
		Schema: schemaStr,
	}); err != nil {
		eventStoreDbErrCounter.WithLabelValues("cassandra", "write").Inc()
		return "", errors.Wrap(err, "Error executing update query in Cassandra")
	}

	es.topicMutex.Lock()
	es.topicNameToId[newName] = es.topicNameToId[oldName]
	es.topicIdToName[id] = newName
	if newName != oldName {
		delete(es.topicNameToId, oldName)
	}
	es.topicSchemaMap[id] = jsonSchema
	es.topicSchemaPropertiesMap[id] = schema
	es.topicMutex.Unlock()

	fmt.Println("Topic Updated:", newName, id)
	return id, nil
}

func (es *EventStore) DeleteTopic(deleteReq *eventmaster.DeleteTopicRequest) error {
	start := time.Now()
	defer func() {
		eventStoreTimer.WithLabelValues("DeleteTopic").Observe(trackTime(start))
	}()

	topicName := strings.ToLower(deleteReq.TopicName)
	id := es.getTopicId(topicName)
	if id == "" {
		return errors.New("Couldn't find topic id for topic:" + topicName)
	}

	if err := es.ds.DeleteTopic(id); err != nil {
		eventStoreDbErrCounter.WithLabelValues("cassandra", "write").Inc()
		return errors.Wrap(err, "Error executing delete query in Cassandra")
	}

	es.topicMutex.Lock()
	delete(es.topicNameToId, topicName)
	delete(es.topicIdToName, id)
	delete(es.topicSchemaMap, id)
	delete(es.topicSchemaPropertiesMap, id)
	es.topicMutex.Unlock()

	fmt.Println("Topic Deleted:", topicName, id)
	return nil
}

func (es *EventStore) AddDc(dc *eventmaster.Dc) (string, error) {
	start := time.Now()
	defer func() {
		eventStoreTimer.WithLabelValues("AddDc").Observe(trackTime(start))
	}()

	name := strings.ToLower(dc.DcName)
	if name == "" {
		return "", errors.New("Error adding dc - dc name is empty")
	}
	id := es.getDcId(name)
	if id != "" {
		return "", errors.New(fmt.Sprintf("Error adding dc - dc with name %s already exists", dc))
	}

	id = uuid.NewV4().String()
	if err := es.ds.AddDc(Dc{
		ID:   id,
		Name: name,
	}); err != nil {
		eventStoreDbErrCounter.WithLabelValues("cassandra", "write").Inc()
		return "", errors.Wrap(err, "Error adding dc to data source")
	}

	es.dcMutex.Lock()
	es.dcIdToName[id] = name
	es.dcNameToId[name] = id
	es.dcMutex.Unlock()

	fmt.Println("Dc Added:", name, id)
	return id, nil
}

func (es *EventStore) UpdateDc(updateReq *eventmaster.UpdateDcRequest) (string, error) {
	start := time.Now()
	defer func() {
		eventStoreTimer.WithLabelValues("UpdateDc").Observe(trackTime(start))
	}()

	oldName := updateReq.OldName
	newName := updateReq.NewName

	if newName == "" {
		return "", errors.New("Dc name cannot be empty")
	}
	if oldName == newName {
		return "", errors.New("There are no changes to be made")
	}

	id := es.getDcId(newName)
	if id != "" {
		return "", errors.New(fmt.Sprintf("Error updating dc - dc with name %s already exists", newName))
	}
	id = es.getDcId(oldName)
	if id == "" {
		return "", errors.New(fmt.Sprintf("Error updating dc - dc with name %s doesn't exist", oldName))
	}
	if err := es.ds.UpdateDc(id, newName); err != nil {
		eventStoreDbErrCounter.WithLabelValues("cassandra", "write").Inc()
		return "", errors.Wrap(err, "Error executing update query in data source")
	}

	es.dcMutex.Lock()
	es.dcNameToId[newName] = es.dcNameToId[oldName]
	es.dcIdToName[id] = newName
	if newName != oldName {
		delete(es.dcNameToId, oldName)
	}
	es.dcMutex.Unlock()

	fmt.Println("Dc Updated:", newName, id)
	return id, nil
}

func (es *EventStore) Update() error {
	start := time.Now()
	defer func() {
		eventStoreTimer.WithLabelValues("Update").Observe(trackTime(start))
	}()

	// Update Dc maps
	newDcNameToId := make(map[string]string)
	newDcIdToName := make(map[string]string)
	dcs, err := es.ds.GetDcs()
	if err != nil {
		eventStoreDbErrCounter.WithLabelValues("cassandra", "read").Inc()
		return errors.Wrap(err, "Error closing dc iter")
	}
	for _, dc := range dcs {
		newDcNameToId[dc.Name] = dc.ID
		newDcIdToName[dc.ID] = dc.Name
	}
	if newDcNameToId != nil {
		es.dcMutex.Lock()
		es.dcNameToId = newDcNameToId
		es.dcIdToName = newDcIdToName
		es.dcMutex.Unlock()
	}

	// Update Topic maps
	newTopicNameToId := make(map[string]string)
	newTopicIdToName := make(map[string]string)
	schemaMap := make(map[string]string)
	newTopicSchemaMap := make(map[string]*gojsonschema.Schema)
	newTopicSchemaPropertiesMap := make(map[string](map[string]interface{}))
	topics, err := es.ds.GetTopics()
	if err != nil {
		eventStoreDbErrCounter.WithLabelValues("cassandra", "read").Inc()
		return errors.Wrap(err, "Error closing topic iter")
	}
	for _, t := range topics {
		newTopicNameToId[t.Name] = t.ID
		newTopicIdToName[t.ID] = t.Name
		bytes, err := json.Marshal(t.Schema)
		if err != nil {
			bytes = []byte("")
		}
		schemaMap[t.ID] = string(bytes)
	}
	for id, schema := range schemaMap {
		if schema != "" {
			var s map[string]interface{}
			if err := json.Unmarshal([]byte(schema), &s); err != nil {
				return errors.Wrap(err, "Error unmarshalling json schema")
			}

			schemaLoader := gojsonschema.NewStringLoader(schema)
			jsonSchema, err := gojsonschema.NewSchema(schemaLoader)
			if err != nil {
				return errors.Wrap(err, "Error validating schema for topic "+id)
			}
			newTopicSchemaMap[id] = jsonSchema
			newTopicSchemaPropertiesMap[id] = s
		}
	}
	es.topicMutex.Lock()
	es.topicNameToId = newTopicNameToId
	es.topicIdToName = newTopicIdToName
	es.topicSchemaMap = newTopicSchemaMap
	es.topicSchemaPropertiesMap = newTopicSchemaPropertiesMap
	es.topicMutex.Unlock()
	return nil
}

func (es *EventStore) CloseSession() {
	es.ds.CloseSession()
}