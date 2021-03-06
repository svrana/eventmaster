var params = [];
var querySuccess = true;
var curPage = 0;

function updateResults() {
    params = params.filter(function(v) {
        return !v.startsWith('limit=')
    });
    var idQuery = params.filter(function(v) {
        return v.startsWith('event_id=')
    })
    if (idQuery.length >0) {
        $.ajax({
            type: "GET",
            url: "/v1/event/"+idQuery[0].substr(9),
            dataType: "json",
            success: function(data) {
                querySuccess = true;
                var elem = document.getElementById("event_table")
                elem.innerHTML = "";
                var event = data["result"];
                if (event) {
                    var item =
                    `<tr onclick=hideData(this)>
                        <td style="word-wrap:break-word;overflow:hidden;">`.concat(event['event_id'],`</td>
                        <th style="word-wrap:break-word;overflow:hidden;" scope="row">`,event['topic_name'],`</th>
                        <td style="word-wrap:break-word;overflow:hidden;">`,event['dc'],`</td>
                        <td style="word-wrap:break-word;overflow:hidden;">`,(event['tag_set'] || []).join(", "),`</td>
                        <td style="word-wrap:break-word;overflow:hidden;">`,new Date(event['event_time']*1000).toString(),`</td>
                        <td style="word-wrap:break-word;overflow:hidden;">`,event['host'],`</td>
                        <td style="word-wrap:break-word;overflow:hidden;">`,(event['target_host_set'] || []).join(", "),`</td>
                        <td style="word-wrap:break-word;overflow:hidden;">`,event['user'],`</td>
                        <td style="word-wrap:break-word;overflow:hidden;">`,event['parent_event_id'],`</td>
                    </tr>
                    <tr>
                        <td colspan="9" style="word-wrap:break-word;overflow:hidden;"><pre></pre></td>
                    </tr>`)
                    elem.innerHTML += item;
                    $("td[colspan=9]").find("pre").hide();
                }
            },
            error: function(data) {
                querySuccess = false;
                alert("Error querying events: " + JSON.parse(data.responseText).error);
            },
            complete: function(data) {
                $('#loading-indicator').hide();
            }
        });
    } else {
        params.push('limit=100');
        $.ajax({
            type: "GET",
            url: "/v1/event?"+params.join("&"),
            dataType: "json",
            success: function(data) {
                querySuccess = true;
                var elem = document.getElementById("event_table")
                elem.innerHTML = "";
                var results = data["results"];
                if (results && results.length > 0) {
                    for (var i = 0; i < results.length; i++) {
                        var event = results[i];
                        var item =
                        `<tr onclick=hideData(this)>
                            <td style="word-wrap:break-word;overflow:hidden;">`.concat(event['event_id'],`</td>
                            <th style="word-wrap:break-word;overflow:hidden;" scope="row">`,event['topic_name'],`</th>
                            <td style="word-wrap:break-word;overflow:hidden;">`,event['dc'],`</td>
                            <td style="word-wrap:break-word;overflow:hidden;">`,(event['tag_set'] || []).join(", "),`</td>
                            <td style="word-wrap:break-word;overflow:hidden;">`,new Date(event['event_time']*1000).toString(),`</td>
                            <td style="word-wrap:break-word;overflow:hidden;">`,event['host'],`</td>
                            <td style="word-wrap:break-word;overflow:hidden;">`,(event['target_host_set'] || []).join(", "),`</td>
                            <td style="word-wrap:break-word;overflow:hidden;">`,event['user'],`</td>
                            <td style="word-wrap:break-word;overflow:hidden;">`,event['parent_event_id'],`</td>
                        </tr>
                        <tr>
                            <td colspan="9" style="word-wrap:break-word;overflow:hidden;"><pre></pre></td>
                        </tr>`)
                        elem.innerHTML += item;
                        $("td[colspan=9]").find("pre").hide();
                    }
                }
            },
            error: function(data) {
                querySuccess = false;
                alert("Error querying events: " + JSON.parse(data.responseText).error);
            },
            complete: function(data) {
                $('#loading-indicator').hide();
            }
        });
    }

}

function backgroundUpdate() {
	if (querySuccess && document.getElementById("refreshCheckbox").checked) {
		updateResults()
	}
	setTimeout(backgroundUpdate, 5000)
}

function getShareableLink() {
    var parser = document.createElement('a');
    parser.href = document.URL;
    var link = parser.origin + "?" + params.join("&");
    prompt("Shareable Link:", link);
}

function clearQuery() {
    params = [];
    document.getElementById("event_id").value = "";
    document.getElementById("parent_event_id").value = "";
    document.getElementById("dc").value = "";
    var topics = document.getElementById("topic-select-box").options;
    for (var i = 0; i < topics.length; i++) {
        $("#topic-select-box").multiselect('deselect', [topics[i].value]);
    }
    document.getElementById("tag_and_operator").checked = false;
    document.getElementById("tag_set").value = "";
    document.getElementById("exclude_tag_set").value = "";
    document.getElementById("host").value = "";
    document.getElementById("tag_host_and_operator").checked = false;
    document.getElementById("target_host_set").value = "";
    document.getElementById("user").value = "";
    document.getElementById("data").value = "";
    document.getElementById("start-event-time").value = "";
    document.getElementById("end-event-time").value = "";
    updateResults();
    curPage = 0;
}

function getTimestampStr(unixTimestamp) {
    if (!unixTimestamp) {
        return ""
    }
    var date = new Date(unixTimestamp * 1000);
    diff = date.getTimezoneOffset() / 60 - offset
    if (diff !== 0) {
        newTime = unixTimestamp - diff*60*60
        date = new Date(newTime * 1000);
    }
    var suffix = "AM";
    var hours = date.getHours();
    if (hours >= 12) {
        suffix = "PM";
        hours = hours - 12;
    }
    return (date.getMonth()+1).toString() + "/" + date.getDate().toString() + "/" + date.getFullYear().toString() + " " + hours.toString() + ":" + date.getMinutes().toString() + " " + suffix;
}

$(document).ready(function() {
    $('#starttimepicker').datetimepicker();
    $('#endtimepicker').datetimepicker();
    $('#topic-select-box').multiselect({
        enableFiltering: true,
        includeSelectAllOption: true,
        numberDisplayed: 1,
        selectAllNumber: false,
        enableCaseInsensitiveFiltering: true,
        buttonWidth: '100%'
    });
    $('#query-form').submit();
    // re-enable this when performance for Cassandra is better along with checkbox in UI
	// backgroundUpdate();
});

$("#menu-toggle").click(function(e) {
	e.preventDefault();
	$("#wrapper").toggleClass("toggled");
});

function hideData(row) {
    // document.getElementById("refreshCheckbox").checked = false;
    var id = $(row).find("td:first").html();
    var nextRow = $(row).next().find("pre");
    if ($(nextRow).html() === "") {
        $.ajax({
            type: "GET",
            url: "/v1/event/"+id,
            dataType: "json",
            success: function(data) {
                var event = data["result"];
                if (event) {
                    $(row).next().find("pre").text(JSON.stringify(event['data'],null,4))
                }
            },
            error: function(data) {
                alert("Error getting event data: " + JSON.parse(data.responseText).error);
            },
        });
    }
    $(row).next().find("pre").slideToggle();
}

function submitQuery(form) {
    $('#loading-indicator').show();
	var data = $(form).serializeArray();
	var formData = {};
	var startEventTime, endEventTime;
	var topics = [];
	for (var i = 0; i < data.length; i++) {
		var key = data[i]["name"];
		var value = data[i]["value"];
		if (value) {
            if (key.endsWith("operator")) {
            	formData[key] = "true"
            } else {
                switch(key) {
                    case "selected_topics[]":
                        topics.push(value);
                        break;
				    case "data":
					    formData[key] = value;
					    break;
				    case "startEventTime":
					    startEventTime = value;
					    break;
				    case "endEventTime":
					    endEventTime = value;
					    break;
				    default:
				    	formData[key] = value === "" ? [] : value.replace(/\s+/g, '').split(",");
                }
			}
		}
	}
    if (topics.length > 0) {
        formData["topic_name"] = topics;
    }

	if (startEventTime) {
	    formData["start_event_time"] = getTimestamp(startEventTime);
	}
	if (endEventTime) {
	    formData["end_event_time"] = getTimestamp(endEventTime);
	}
	params = [];
	for (var key in formData) {
		if (key === "start_event_time" || key === "end_event_time" || key == "data"
			|| key === "tag_and_operator" || key === "target_host_and_operator") {
			if (formData[key] != "") {
				params.push(key + "=" + formData[key])
			}
		} else {
			for (var j = 0; j < formData[key].length; j++) {
				params.push(key + "=" + formData[key][j])
			}
		}
	}
    curPage=0;
	updateResults()
	return false;
}

function loadQueryTimes(start, end) {
    if (start) {
        document.getElementById('start-event-time').value = getTimestampStr(start);
    } else {
        document.getElementById('start-event-time').value = getTimestampStr(Date.now()/1000-5000);
    }
    if (end) {
        document.getElementById('end-event-time').value = getTimestampStr(end);
    } else {
        document.getElementById('end-event-time').value = getTimestampStr(Date.now()/1000+60);
    }
}

