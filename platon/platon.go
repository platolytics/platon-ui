package platon

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

func CreatePromClient(promAddress string) (api.Client, error) {
	client, err := api.NewClient(api.Config{
		Address: promAddress,
	})

	if err != nil {
		return nil, fmt.Errorf("Error creating client: %v\n", err)
	}
	return client, nil
}

func GetMetrics(client api.Client, startTime time.Time, endTime time.Time) (model.LabelValues, error) {
	v1api := v1.NewAPI(client)
	labels, warnings, err := v1api.LabelValues(context.Background(), "__name__", []string{}, startTime, endTime)
	// Always log the warnings even if errors cause crash
	if len(warnings) > 0 {
		fmt.Printf("Warnings: %v\n", warnings)
	}
	if err != nil {
		return nil, err
	}
	return labels, nil
}

func GetSamples(client api.Client, metric string, startTime time.Time, endTime time.Time) (model.Value, error) {
	v1api := v1.NewAPI(client)

	result, warnings, err := v1api.QueryRange(context.TODO(), metric, v1.Range{Start: startTime, End: endTime, Step: 1 * time.Minute}, v1.WithTimeout(5*time.Second))
	// Always log the warnings even if errors cause crash
	if len(warnings) > 0 {
		fmt.Printf("Warnings: %v\n", warnings)
	}
	if err != nil {
		return nil, fmt.Errorf("Error querying Prometheus: %v\n", err)
	}

	return result, nil
}

// GetQueryTimes sets startTime to now and endTime one hour in the past
func GetQueryTimes() (time.Time, time.Time) {
	startTime := time.Now().Add(-1 * time.Hour)
	endTime := time.Now()
	return startTime, endTime
}

// ConstructURL builds a URL and returns it as string
func ConstructURL(address string, port string, ssl bool) string {
	var url string
	if ssl {
		url = "https://" + address + ":" + port
	}
	if !ssl {
		url = "http://" + address + ":" + port
	}
	return url
}

func MetricsToTable(queryResults []model.Value) map[string][]string {
	tableheaders := FillColumnHeads(queryResults)

	data := map[string][]string{}
	for _, metrics := range queryResults {
		matrix := metrics.(model.Matrix)
		for _, sampleStream := range matrix {
			for _, value := range sampleStream.Values {
				row := 0
				newRow := true
				time := fmt.Sprintf("%v", value.Timestamp)
				if slices.Contains(data["time"], time) {
					row = slices.Index(data["time"], time)
					newRow = false
				}
				for dimension := range tableheaders {
					if newRow {
						if dimension == "time" {
							data[dimension] = append(data[dimension], time)
							continue
						}
						if dimension == string(sampleStream.Metric["__name__"]) {
							data[dimension] = append(data[dimension], value.Value.String())
							continue
						}
						for label, value := range sampleStream.Metric {
							if dimension == string(label) {
								data[dimension] = append(data[dimension], string(value))
								continue
							}
						}
						data[dimension] = append(data[dimension], "")
					} else {
						if dimension == string(sampleStream.Metric["__name__"]) {
							data[dimension][row] = value.Value.String()
							continue
						}
						if data[dimension][row] == "" {
							for label, value := range sampleStream.Metric {
								if dimension == string(label) {
									data[dimension][row] = string(value)
									continue
								}
							}
						}
					}
				}
			}
		}
	}
	return data
}

func FillColumnHeads(queryResults []model.Value) map[string]bool {

	// dimensions == {"time":true, "value":true}
	dimensions := map[string]bool{
		"time": true,
	}
	for _, metrics := range queryResults {
		// map results into matrix
		matrix := metrics.(model.Matrix)

		for _, sampleStream := range matrix {
			for label, value := range sampleStream.Metric {
				// We want to make the name a column
				if string(label) != "__name__" {
					dimensions[string(label)] = true
				}
				// Add metric name as column
				if string(label) == "__name__" {
					dimensions[string(value)] = true
				}
			}
		}
	}

	// dimensions at this point should hold all label names + true like so
	// dimensions = {"time": true, "value": true, "label1":true, "label2": true, ...}
	return dimensions
}
