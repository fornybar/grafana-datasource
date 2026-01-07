package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/feldera/feldera/pkg/models"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Make sure Datasource implements required interfaces. This is important to do
// since otherwise we will only get a not implemented error response from plugin in
// runtime. In this example datasource instance implements backend.QueryDataHandler,
// backend.CheckHealthHandler interfaces. Plugin should not implement all these
// interfaces - only those which are required for a particular task.
var (
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

// NewDatasource creates a new datasource instance.
func NewDatasource(_ context.Context, setting backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	settings, err := models.LoadPluginSettings(setting)
	if err != nil {
		return nil, err
	}

	return &Datasource{
		client:   http.Client{},
		pipeline: settings.Pipeline,
		baseUrl:  settings.BaseUrl,
	}, nil
}

// Datasource is an example datasource which can respond to data queries, reports
// its health and has streaming skills.
type Datasource struct {
	client   http.Client
	baseUrl  string
	pipeline string
}

// Dispose here tells plugin SDK that plugin wants to clean up resources when a new instance
// created. As soon as datasource settings change detected by SDK old datasource instance will
// be disposed and a new one will be created using NewSampleDatasource factory function.
func (d *Datasource) Dispose() {
	// Clean up datasource instance resources.
}

// QueryData handles multiple queries and returns multiple responses.
// req contains the queries []DataQuery (where each query contains RefID as a unique identifier).
// The QueryDataResponse contains a map of RefID to the response for each query, and each response
// contains Frames ([]*Frame).
func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	// create response struct
	response := backend.NewQueryDataResponse()

	// loop over queries and execute them individually.
	for _, q := range req.Queries {
		res := d.query(ctx, req.PluginContext, q)

		// save the response in a hashmap
		// based on with RefID as identifier
		response.Responses[q.RefID] = res
	}

	return response, nil
}

type queryModel struct {
	QueryText string `json:"queryText"`
}

func (d *Datasource) query(_ context.Context, pCtx backend.PluginContext, query backend.DataQuery) backend.DataResponse {
	var response backend.DataResponse

	// Unmarshal the JSON into our queryModel.
	var qm queryModel

	// TODO: query.TimeRange
	// query.MaxDataPoints
	// query.Interval

	err := json.Unmarshal(query.JSON, &qm)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusValidationFailed, fmt.Sprintf("json unmarshal: %v", err.Error()))
	}

	sql := qm.QueryText

	if sql == "" {
		return response
	}

	sql = strings.ReplaceAll(sql, "$__timeFrom()", fmt.Sprintf("'%s'", query.TimeRange.From.UTC().Format(time.RFC3339)))
	sql = strings.ReplaceAll(sql, "$__timeTo()", fmt.Sprintf("'%s'", query.TimeRange.To.UTC().Format(time.RFC3339)))

	println(sql)

	params := url.Values{}
	params.Add("format", "json")
	params.Add("sql", sql)

	url := fmt.Sprintf("%s/v0/pipelines/%s/query?%s", d.baseUrl, d.pipeline, params.Encode())

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("failed to create http request: %v", err.Error()))
	}

	if pCtx.DataSourceInstanceSettings != nil {
		config, err := models.LoadPluginSettings(*pCtx.DataSourceInstanceSettings)
		if err != nil {
			return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("failed to load http config: %v", err.Error()))
		}

		apiKey := config.Secrets.ApiKey
		if apiKey != "" {
			req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", apiKey))
		}
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadGateway, fmt.Sprintf("feldera error: %v", err.Error()))
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		msg, err := io.ReadAll(resp.Body)
		if err != nil {
			return backend.ErrDataResponse(
				backend.StatusBadGateway,
				fmt.Sprintf("err: query failed, status: %s", resp.Status))
		}
		errMsg := string(msg)

		return backend.ErrDataResponse(backend.StatusBadRequest,
			fmt.Sprintf("err: query failed, status: '%s', error: %s", resp.Status, errMsg))
	}

	contents, err := io.ReadAll(resp.Body)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal,
			fmt.Sprintf("err: failed to read response body: %v", err.Error()))
	}

	// Handle empty response (no rows)
	if len(strings.TrimSpace(string(contents))) == 0 {
		response.Frames = append(response.Frames, data.NewFrame("response"))
		return response
	}

	frame, err := parseJSONResponse(contents)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal,
			fmt.Sprintf("err: failed to parse response: %v", err.Error()))
	}

	response.Frames = append(response.Frames, frame)

	return response
}

// parseJSONResponse parses Feldera's newline-delimited JSON and converts timestamps
func parseJSONResponse(contents []byte) (*data.Frame, error) {
	jsonStr := "[" + strings.TrimSpace(string(contents))
	jsonStr = strings.ReplaceAll(jsonStr, "\n", ", ") + "]"

	var rows []map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &rows); err != nil {
		return nil, fmt.Errorf("json unmarshal: %v", err)
	}

	if len(rows) == 0 {
		return data.NewFrame("response"), nil
	}

	var colNames []string
	for name := range rows[0] {
		colNames = append(colNames, name)
	}

	fields := make([]*data.Field, len(colNames))
	for i, name := range colNames {
		values := make([]any, len(rows))
		for j, row := range rows {
			values[j] = row[name]
		}
		fields[i] = createField(name, values)
	}

	frame := data.NewFrame("response", fields...)
	return frame, nil
}

func createField(name string, values []any) *data.Field {
	if len(values) == 0 {
		return data.NewField(name, nil, []*string{})
	}

	if isTimestampColumn(values) {
		times := make([]*time.Time, len(values))
		for i, v := range values {
			if v == nil {
				times[i] = nil
				continue
			}
			if s, ok := v.(string); ok {
				if t, err := parseTimestamp(s); err == nil {
					times[i] = &t
				}
			}
		}
		return data.NewField(name, nil, times)
	}

	if isNumericColumn(values) {
		nums := make([]*float64, len(values))
		for i, v := range values {
			if v == nil {
				nums[i] = nil
				continue
			}
			if n, ok := v.(float64); ok {
				nums[i] = &n
			}
		}
		return data.NewField(name, nil, nums)
	}

	strs := make([]*string, len(values))
	for i, v := range values {
		if v == nil {
			strs[i] = nil
			continue
		}
		s := fmt.Sprintf("%v", v)
		strs[i] = &s
	}
	return data.NewField(name, nil, strs)
}

func isTimestampColumn(values []any) bool {
	for _, v := range values {
		if v == nil {
			continue
		}
		s, ok := v.(string)
		if !ok {
			return false
		}
		if _, err := parseTimestamp(s); err != nil {
			return false
		}
		return true
	}
	return false
}

func parseTimestamp(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05.999",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("not a timestamp: %s", s)
}

func isNumericColumn(values []any) bool {
	for _, v := range values {
		if v == nil {
			continue
		}
		if _, ok := v.(float64); !ok {
			return false
		}
	}
	return true
}

// CheckHealth handles health checks sent from Grafana to the plugin.
// The main use case for these health checks is the test button on the
// datasource configuration page which allows users to verify that
// a datasource is working as expected.
func (d *Datasource) CheckHealth(_ context.Context, req *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	res := &backend.CheckHealthResult{}
	config, err := models.LoadPluginSettings(*req.PluginContext.DataSourceInstanceSettings)

	if err != nil {
		res.Status = backend.HealthStatusError
		res.Message = "Unable to load settings"
		return res, nil
	}

	apiKey := config.Secrets.ApiKey
	if apiKey != "" {
	}

	url := fmt.Sprintf("%s/v0/pipelines", d.baseUrl)

	r, err := http.NewRequest("GET", url, nil)
	if err != nil {
		res.Status = backend.HealthStatusError
		res.Message = "Unable to create HTTP request"
		return res, nil
	}

	resp, err := d.client.Do(r)
	if err != nil {
		res.Status = backend.HealthStatusError
		res.Message = "Data source unavailable"
		return res, nil
	}

	if resp.StatusCode >= 400 || resp.StatusCode < 200 {
		res.Status = backend.HealthStatusError
		res.Message = "Invalid response from data source"
		return res, nil
	}

	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Data source is working",
	}, nil
}
