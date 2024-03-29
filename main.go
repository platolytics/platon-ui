package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/platolytics/platon-ui/platon"
	"github.com/platolytics/platon-ui/templates/home"

	"github.com/apache/arrow/go/v14/arrow"
	"github.com/apache/arrow/go/v14/arrow/memory"

	"github.com/polarsignals/frostdb"
	"github.com/polarsignals/frostdb/query"
	"github.com/polarsignals/frostdb/query/logicalplan"

	"github.com/a-h/templ"
	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/go-echarts/go-echarts/v2/types"
)

// generate random data for bar chart
func generateBarItems() []opts.BarData {
	items := make([]opts.BarData, 0)
	for i := 0; i < 7; i++ {
		items = append(items, opts.BarData{Value: rand.Intn(300)})
	}
	return items
}

func barChart() string {
	// create a new bar instance
	bar := charts.NewBar()
	// set some global options like Title/Legend/ToolTip or anything else
	bar.SetGlobalOptions(charts.WithTitleOpts(opts.Title{
		Title:    "Some bar chart",
		Subtitle: "Rendered with go-echarts",
	}))

	// Put data into instance
	bar.SetXAxis([]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}).
		AddSeries("Category A", generateBarItems()).
		AddSeries("Category B", generateBarItems())

	buf := new(bytes.Buffer)
	bar.Render(buf)
	return buf.String()
}

func lineChart() string {
	// create a new line instance
	line := charts.NewLine()
	// set some global options like Title/Legend/ToolTip or anything else
	line.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{Theme: types.ThemeWesteros}),
		charts.WithTitleOpts(opts.Title{
			Title:    "Some time series data",
			Subtitle: "Rendered with go-echarts",
		}))

	// Put data into instance
	line.SetXAxis([]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}).
		AddSeries("Category A", generateLineItems()).
		AddSeries("Category B", generateLineItems()).
		SetSeriesOptions(charts.WithLineChartOpts(opts.LineChart{Smooth: true}))
	buf := new(bytes.Buffer)
	line.Render(buf)
	return buf.String()

}

// generate random data for line chart
func generateLineItems() []opts.LineData {
	items := make([]opts.LineData, 0)
	for i := 0; i < 7; i++ {
		items = append(items, opts.LineData{Value: rand.Intn(300)})
	}
	return items
}

func snowfall() string {
	// Create a new column store
	columnstore, _ := frostdb.New()
	defer columnstore.Close()

	// Open up a database in the column store
	database, _ := columnstore.DB(context.Background(), "weather_db")

	// Create values to insert into the database. We support a dynamic structure for city to
	// accommodate cities in different regions
	type WeatherRecord struct {
		City     map[string]string `frostdb:",rle_dict,asc(0)"`
		Day      string            `frostdb:",rle_dict,asc(1)"`
		Snowfall float64
	}

	// Create a table named snowfall_table in our database
	table, _ := frostdb.NewGenericTable[WeatherRecord](
		database, "snowfall_table", memory.DefaultAllocator,
	)
	defer table.Release()

	montreal := map[string]string{
		"name":     "Montreal",
		"province": "Quebec",
	}
	toronto := map[string]string{
		"name":     "Toronto",
		"province": "Ontario",
	}
	minneapolis := map[string]string{
		"name":  "Minneapolis",
		"state": "Minnesota",
	}

	_, _ = table.Write(context.Background(),
		WeatherRecord{Day: "Mon", Snowfall: 20, City: montreal},
		WeatherRecord{Day: "Tue", Snowfall: 00, City: montreal},
		WeatherRecord{Day: "Wed", Snowfall: 30, City: montreal},
		WeatherRecord{Day: "Thu", Snowfall: 25.1, City: montreal},
		WeatherRecord{Day: "Fri", Snowfall: 10, City: montreal},
		WeatherRecord{Day: "Mon", Snowfall: 15, City: toronto},
		WeatherRecord{Day: "Tue", Snowfall: 25, City: toronto},
		WeatherRecord{Day: "Wed", Snowfall: 30, City: toronto},
		WeatherRecord{Day: "Thu", Snowfall: 00, City: toronto},
		WeatherRecord{Day: "Fri", Snowfall: 05, City: toronto},
		WeatherRecord{Day: "Mon", Snowfall: 40.8, City: minneapolis},
		WeatherRecord{Day: "Tue", Snowfall: 15, City: minneapolis},
		WeatherRecord{Day: "Wed", Snowfall: 32.3, City: minneapolis},
		WeatherRecord{Day: "Thu", Snowfall: 10, City: minneapolis},
		WeatherRecord{Day: "Fri", Snowfall: 12, City: minneapolis},
	)

	// Create a new query engine to retrieve data
	engine := query.NewEngine(memory.DefaultAllocator, database.TableProvider())

	weekdays := []string{"Mon", "Tue", "Wed", "Thu", "Fri"}
	minneapolisData := getAverageSnow(engine, weekdays, minneapolis["name"])
	montrealData := getAverageSnow(engine, weekdays, montreal["name"])
	torontoData := getAverageSnow(engine, weekdays, toronto["name"])

	line := charts.NewLine()
	line.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{Theme: types.ThemeWesteros}),
		charts.WithTitleOpts(opts.Title{
			Title:    "Average snowfall per weekday",
			Subtitle: "Aggregated in frostdb backend",
		}))

	// Put data into instance
	line.SetXAxis(weekdays).
		AddSeries(minneapolis["name"], minneapolisData).
		AddSeries(toronto["name"], torontoData).
		AddSeries(montreal["name"], montrealData).
		SetSeriesOptions(charts.WithLineChartOpts(opts.LineChart{Smooth: true}))
	buf := new(bytes.Buffer)
	line.Render(buf)
	return buf.String()
}

func getAverageSnow(engine *query.LocalEngine, weekdays []string, city string) []opts.LineData {
	result := make([]opts.LineData, 0)
	err := engine.ScanTable("snowfall_table").
		Filter(logicalplan.Col("city.name").Eq(logicalplan.Literal(city))).
		Aggregate(
			[]*logicalplan.AggregationFunction{
				logicalplan.Avg(logicalplan.Col("snowfall")),
			},
			[]logicalplan.Expr{logicalplan.Col("day")},
		).
		Execute(context.Background(), func(ctx context.Context, r arrow.Record) error {
			fmt.Println(r)

			var dayColumn, valueColumn int
			for colIndex := 0; colIndex < int(r.NumCols()); colIndex++ {
				if r.ColumnName(colIndex) == "day" {
					dayColumn = colIndex
					continue
				}
				if r.ColumnName(colIndex) == "avg(snowfall)" {
					valueColumn = colIndex
					continue
				}
			}
			for _, day := range weekdays {
				for i := 0; i < int(r.NumRows()); i++ {
					if r.Column(dayColumn).GetOneForMarshal(i) == day {
						result = append(result, opts.LineData{Value: r.Column(valueColumn).GetOneForMarshal(int(i))})
					}
				}

			}

			fmt.Println(result)
			return nil
		})
	if err != nil {
		log.Fatal("total snowfall on each day of week:", err)
	}
	return result
}

func prometheusData(platon platon.Platon, cube platon.Cube) string {

	line := charts.NewLine()
	line.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{Theme: types.ThemeWesteros}),
		charts.WithTitleOpts(opts.Title{
			Title:    "Memory metrics from Prometheus",
			Subtitle: "Aggregated in frostdb backend",
		}))

	engine := platon.GetQueryEngine()
	engine.ScanTable(cube.Name).
		Aggregate(
			[]*logicalplan.AggregationFunction{
				logicalplan.Avg(logicalplan.Col("metrics.node_memory_Cached_bytes")),
				logicalplan.Avg(logicalplan.Col("metrics.node_memory_MemFree_bytes")),
			},
			[]logicalplan.Expr{logicalplan.Col("time")},
		).
		Execute(context.Background(), func(ctx context.Context, r arrow.Record) error {
			fmt.Println(r)
			fmt.Println(r.ColumnName(0))
			fmt.Println("----")

			for colIndex := 0; colIndex < int(r.NumCols()); colIndex++ {

				if r.ColumnName(colIndex) == "time" {

					xAxis := []string{}
					for i := 0; i < int(r.NumRows()); i++ {
						timestamp := time.Unix(r.Column(colIndex).GetOneForMarshal(i).(int64)/1000, 0)
						xAxis = append(xAxis, timestamp.Format(time.RFC3339))
					}
					line.SetXAxis(xAxis)
					continue
				}
				columnData := []opts.LineData{}
				for i := 0; i < int(r.NumRows()); i++ {
					columnData = append(columnData, opts.LineData{Value: r.Column(colIndex).GetOneForMarshal(i)})
				}
				line.AddSeries(r.ColumnName(colIndex), columnData)
			}
			return nil
		})

	// Put data into instance
	buf := new(bytes.Buffer)
	line.Render(buf)
	return buf.String()
}

var metrics []string

func loadMetrics() {
	startTime, endTime := platon.GetQueryTimes()
	// start prometheus client
	client, err := platon.GetPromClient()
	if err != nil {
		log.Fatal(err)
	}

	// get all metric names
	metricValues, err := platon.GetMetrics(client, startTime, endTime)
	metrics = []string{}
	if err != nil {
		log.Fatal(err)
	}
	for _, metricValue := range metricValues {
		metrics = append(metrics, string(metricValue))
	}
	fmt.Println("All metrics: \n", metrics)
}

func initExampleCubes() []platon.Cube {
	var cubes []platon.Cube
	cube := platon.Cube{
		Name:    "memory_cube",
		Metrics: []string{"node_memory_Cached_bytes", "node_memory_MemFree_bytes"},
	}
	cubes = append(cubes, cube)
	return cubes
}

//go:generate templ generate
func main() {
	fmt.Println("Starting Platon UI..")
	loadMetrics()
	cubes := initExampleCubes()
	platon := platon.LoadCubes(cubes)
	defer platon.Close()
	static := http.FileServer(http.Dir("./static"))
	http.Handle("/", templ.Handler(home.Page(barChart())))
	http.Handle("/line", templ.Handler(home.Page(lineChart())))
	http.Handle("/weather", templ.Handler(home.Page(snowfall())))
	http.Handle("/prometheus", templ.Handler(home.Page(prometheusData(platon, cubes[0]))))
	http.Handle("/cubes", templ.Handler(home.Cubes(cubes)))
	http.Handle("/metrics", templ.Handler(home.Metrics(metrics)))
	http.Handle("/static/", http.StripPrefix("/static/", static))
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		panic(err)
	}
}
