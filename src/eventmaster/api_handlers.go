package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ContextLogic/eventmaster/eventmaster"
	"github.com/julienschmidt/httprouter"
	"github.com/pkg/errors"
	metrics "github.com/rcrowley/go-metrics"
)

func wrapHandler(h httprouter.Handle, registry metrics.Registry) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		meter := metrics.GetOrRegisterMeter(fmt.Sprintf("%s:%s", r.URL.Path, "Meter"), registry)
		meter.Mark(1)
		start := time.Now()
		timer := metrics.GetOrRegisterTimer(fmt.Sprintf("%s:%s", r.URL.Path, "Timer"), registry)
		defer timer.UpdateSince(start)
		h(w, r, ps)
	}
}

type httpHandler struct {
	store    *EventStore
	registry metrics.Registry
}

func (h *httpHandler) sendError(w http.ResponseWriter, code int, err error, message string, errName string) {
	meter := metrics.GetOrRegisterMeter(errName, h.registry)
	meter.Mark(1)
	errMsg := fmt.Sprintf("%s: %s", message, err.Error())
	fmt.Println(errMsg)
	w.WriteHeader(code)
	w.Write([]byte(errMsg))
}

func (h *httpHandler) sendResp(w http.ResponseWriter, key string, val string, errName string) {
	resp := make(map[string]string)
	resp[key] = val
	str, err := json.Marshal(resp)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, err, "Error marshalling response to JSON", errName)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	w.Write(str)
}

func (h *httpHandler) handleAddEvent(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var evt eventmaster.Event

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&evt); err != nil {
		h.sendError(w, http.StatusBadRequest, err, "Error decoding JSON event", "AddEventError")
		return
	}
	id, err := h.store.AddEvent(&evt)
	if err != nil {
		fmt.Println("Error adding event to store: ", err)
		h.sendError(w, http.StatusBadRequest, err, "Error writing event", "AddEventError")
		return
	}
	h.sendResp(w, "event_id", id, "AddEventError")
}

type SearchResult struct {
	Results []*Event `json:"results"`
}

func (h *httpHandler) handleGetEvent(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var q eventmaster.Query

	// read from request body first - if there's an error, read from query params
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&q); err != nil {
		query := r.URL.Query()
		q.Dc = query["dc"]
		q.Host = query["host"]
		q.TargetHost = query["target_host"]
		q.TagSet = query["tag"]
		q.TopicName = query["topic_name"]
		q.SortField = query["sort_field"]
		for _, elem := range query["sort_ascending"] {
			if strings.ToLower(elem) == "true" {
				q.SortAscending = append(q.SortAscending, true)
			} else if strings.ToLower(elem) == "false" {
				q.SortAscending = append(q.SortAscending, false)
			}
		}
		if len(q.SortField) != len(q.SortAscending) {
			h.sendError(w, http.StatusBadRequest, errors.New("sort_field and sort_ascending don't match"), "Error", "GetEventError")
			return
		}
		if len(query["data"]) > 0 {
			q.Data = query["data"][0]
		}
		if startEventTime := query.Get("start_event_time"); startEventTime != "" {
			q.StartEventTime, _ = strconv.ParseInt(startEventTime, 10, 64)
		}
		if endEventTime := query.Get("end_event_time"); endEventTime != "" {
			q.EndEventTime, _ = strconv.ParseInt(endEventTime, 10, 64)
		}
		if startReceivedTime := query.Get("start_received_time"); startReceivedTime != "" {
			q.StartReceivedTime, _ = strconv.ParseInt(startReceivedTime, 10, 64)
		}
		if endReceivedTime := query.Get("end_received_time"); endReceivedTime != "" {
			q.EndReceivedTime, _ = strconv.ParseInt(endReceivedTime, 10, 64)
		}
		if from := query.Get("from"); from != "" {
			fromIndex, _ := strconv.ParseInt(from, 10, 32)
			q.From = int32(fromIndex)
		}
		if size := query.Get("size"); size != "" {
			resultSize, _ := strconv.ParseInt(size, 10, 32)
			q.Size = int32(resultSize)
		}
	}

	results, err := h.store.Find(&q)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, err, "Error executing query", "GetEventError")
		return
	}
	sr := SearchResult{
		Results: results,
	}
	jsonSr, err := json.Marshal(sr)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, err, "Error marshalling results into JSON", "GetEventError")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonSr)
}

func (h *httpHandler) handleAddTopic(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var td TopicData
	reqBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err, "Error reading request body", "AddTopicError")
		return
	}

	if err = json.Unmarshal(reqBody, &td); err != nil {
		h.sendError(w, http.StatusBadRequest, err, "Error JSON decoding body of request", "AddTopicError")
		return
	}

	id, err := h.store.AddTopic(td.Name, td.Schema)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err, "Error adding topic", "AddTopicError")
		return
	}
	h.sendResp(w, "topic_id", id, "AddTopicError")
}

func (h *httpHandler) handleUpdateTopic(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	var td TopicData
	reqBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err, "Error reading request body", "UpdateTopicError")
		return
	}
	err = json.Unmarshal(reqBody, &td)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err, "Error JSON decoding body of request", "UpdateTopicError")
		return
	}

	topicName := ps.ByName("name")
	if topicName == "" {
		h.sendError(w, http.StatusBadRequest, err, "Error updating topic, no topic name provided", "UpdateTopicError")
		return
	}
	id, err := h.store.UpdateTopic(topicName, td.Name, td.Schema)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err, "Error updating topic", "UpdateTopicError")
		return
	}
	h.sendResp(w, "topic_id", id, "UpdateTopicError")
}

func (h *httpHandler) handleGetTopic(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	topicSet := make(map[string][]TopicData)
	topics, err := h.store.GetTopics()
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, err, "Error getting topics from store", "GetTopicError")
		return
	}
	topicSet["results"] = topics
	str, err := json.Marshal(topicSet)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, err, "Error marshalling response to JSON", "GetTopicError")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(str)
}

func (h *httpHandler) handleAddDc(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var dd DcData
	reqBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err, "Error reading request body", "AddDcError")
		return
	}
	err = json.Unmarshal(reqBody, &dd)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err, "Error JSON decoding body of request", "AddDcError")
		return
	}
	id, err := h.store.AddDc(dd.Name)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err, "Error adding dc", "AddDcError")
		return
	}
	h.sendResp(w, "dc_id", id, "AddDcError")
}

func (h *httpHandler) handleUpdateDc(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	var dd DcData
	reqBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err, "Error reading request body", "UpdateDcError")
		return
	}
	err = json.Unmarshal(reqBody, &dd)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err, "Error JSON decoding body of request", "UpdateDcError")
		return
	}
	dcName := ps.ByName("name")
	if dcName == "" {
		h.sendError(w, http.StatusBadRequest, err, "Error updating topic, no topic name provided", "UpdateDcError")
		return
	}
	id, err := h.store.UpdateDc(dcName, dd.Name)
	if err != nil {
		h.sendError(w, http.StatusBadRequest, err, "Error updating dc", "UpdateDcError")
		return
	}
	h.sendResp(w, "dc_id", id, "UpdateDcError")
}

func (h *httpHandler) handleGetDc(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	dcSet := make(map[string][]string)
	dcs, err := h.store.GetDcs()
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, err, "Error getting dcs from store", "GetDcError")
		return
	}
	dcSet["results"] = dcs
	str, err := json.Marshal(dcSet)
	if err != nil {
		h.sendError(w, http.StatusInternalServerError, err, "Error marshalling response to JSON", "GetDcError")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(str)
}
