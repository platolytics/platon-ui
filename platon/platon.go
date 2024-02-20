package platon

import (
	"context"
	"flag"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/apache/arrow/go/v14/arrow/memory"
	"github.com/polarsignals/frostdb"
	"github.com/polarsignals/frostdb/query"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

var promClient api.Client
var promClientInitialized bool = false

func GetPromClient() (api.Client, error) {
	if promClientInitialized {
		return promClient, nil
	}
	address := flag.String("address", "localhost", "Prometheus address")
	port := flag.String("port", "9090", "Prometheus port")
	isSSL := flag.Bool("ssl", false, "Enable transport security")

	url := ConstructURL(*address, *port, *isSSL)
	var err error
	promClient, err = api.NewClient(api.Config{
		Address: url,
	})

	if err != nil {
		return nil, fmt.Errorf("error creating client: %v", err)
	}
	promClientInitialized = true

	return promClient, nil
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

type Entry struct {
	Labels  map[string]string
	Metrics map[string]float64
	Time    int64
}

func NewEntry(time int64) *Entry {
	return &Entry{
		Time:    time,
		Labels:  map[string]string{},
		Metrics: map[string]float64{},
	}
}

type Cube struct {
	Name        string
	Description string
	LastRefresh string
	Labels      []string
	Metrics     []string
}

func MetricsToTable(queryResults []model.Value, cube Cube, database *frostdb.DB) error {
	cube.Labels, cube.Metrics = FillColumnHeads(queryResults)

	timeData := []int64{}
	entries := []Entry{}
	for _, metrics := range queryResults {
		matrix := metrics.(model.Matrix)
		for _, sampleStream := range matrix {
			for _, value := range sampleStream.Values {
				time := int64(value.Timestamp)
				entry := NewEntry(time)
				if slices.Contains(timeData, time) {
					row := slices.Index(timeData, time)
					entry = &entries[row]
				} else {
					timeData = append(timeData, time)
					entries = append(entries, *entry)
				}
				for _, dimension := range cube.Metrics {
					if dimension == string(sampleStream.Metric["__name__"]) {
						entry.Metrics[dimension] = float64(value.Value)
						break
					}
				}
				for _, dimension := range cube.Labels {
					for label, value := range sampleStream.Metric {
						if dimension == string(label) {
							entry.Labels[dimension] = string(value)
							break
						}
					}
				}
			}
		}
	}
	if len(entries) != len(timeData) {
		return fmt.Errorf("data load error: Inconsistent cube data")
	}

	dbtable, err := frostdb.NewGenericTable[Entry](
		database, cube.Name, memory.DefaultAllocator,
	)
	if err != nil {
		return fmt.Errorf("failed to create db table: %v", err)
	}
	dbtable.Write(context.Background(), entries...)
	return nil
}

func FillColumnHeads(queryResults []model.Value) (labelNames, metricNames []string) {

	metricNames = []string{}
	labelNames = []string{}
	for _, metrics := range queryResults {
		// map results into matrix
		matrix := metrics.(model.Matrix)

		for _, sampleStream := range matrix {
			for label, value := range sampleStream.Metric {
				// We want to make the name a column
				if string(label) != "__name__" {
					if !slices.Contains(labelNames, string(label)) {
						labelNames = append(labelNames, string(label))
					}
				}
				// Add metric name as column
				if string(label) == "__name__" {
					if !slices.Contains(metricNames, string(value)) {
						metricNames = append(metricNames, string(value))
					}
				}
			}
		}
	}

	return
}

type Platon struct {
	db *frostdb.DB
}

func LoadCubes(cubes []Cube) Platon {
	platon := Platon{}
	startTime, endTime := GetQueryTimes()
	// start prometheus client
	client, err := GetPromClient()
	if err != nil {
		log.Fatal(err)
	}

	columnstore, _ := frostdb.New()

	platon.db, _ = columnstore.DB(context.Background(), "simple_db")

	for i := range cubes {
		cube := &cubes[i]

		var queryResults []model.Value

		for _, label := range cube.Metrics {
			queryResult, err := GetSamples(client, string(label), startTime, endTime)
			if err != nil {
				panic(err)
			}
			queryResults = append(queryResults, queryResult)
		}

		cube.Labels, cube.Metrics = FillColumnHeads(queryResults)

		err := MetricsToTable(queryResults, cubes[i], platon.db)
		if err != nil {
			panic(err)
		}
	}

	return platon
}

func (p *Platon) GetQueryEngine() *query.LocalEngine {
	return query.NewEngine(memory.DefaultAllocator, p.db.TableProvider())
}

func (p *Platon) Close() {
	p.db.Close()
}
