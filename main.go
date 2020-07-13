package main

import (
	"encoding/csv"
	"flag"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/perf/benchstat"
)

var (
	commitDates = flag.String("commit-dates", "", "file containing `git log --format=\"format:%H,%cD\" --date-order`")
	metricName  = flag.String("metric", "ns/op", "metric to fetch")
)

const rfc2822 = "Mon, 2 Jan 2006 15:04:05 -0700"


// Get started with:
// * mkdir -p /tmp/bench
// * gsutil -m cp -r 'gs://istio-prow/benchmarks/*.txt' /tmp/bench
// * git log --format="format:%H,%cD" --date-order > /tmp/bench/commits
// * go run main.go --commit-dates=/tmp/commits /tmp/bench/*.txt
func main() {
	flag.Parse()

	if commitDates == nil || *commitDates == "" {
		log.Fatal("require commit-dates")
	}
	c := &benchstat.Collection{
		DeltaTest: benchstat.UTest,
		Alpha:     0.05,
	}
	f, err := ioutil.ReadFile(*commitDates)
	if err != nil {
		log.Fatal(err)
	}

	commitToDate := map[string]time.Time{}
	for _, l := range strings.Split(string(f), "\n") {
		if len(l) == 0 {
			continue
		}
		spl := strings.SplitN(l, ",", 2)
		if len(spl) != 2 {
			log.Fatalf("unexpected split from %v\n", l)
		}
		tm, err := time.Parse(rfc2822, spl[1])
		if err != nil {
			log.Fatalf("failed to parse from %v: %v\n", l, err)
		}
		commitToDate[spl[0]] = tm
	}
	files := flag.Args()
	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			log.Fatal(err)
		}
		if err := c.AddFile(file, f); err != nil {
			log.Fatal(err)
		}
		f.Close()
	}

	fileToDate := func(file string) time.Time {
		sha := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		return commitToDate[sha]
	}

	results := map[string]map[time.Time]Result{}
	for _, t := range c.Tables() {
		for _, row := range t.Rows {
			for i, metric := range row.Metrics {
				if metric.Unit != *metricName {
					continue
				}
				date := fileToDate(files[i])
				result := Result{Name: row.Benchmark, Date: date, Nanoseconds: metric.Mean}
				if _, f := results[row.Benchmark]; !f {
					results[row.Benchmark] = map[time.Time]Result{}
				}
				results[row.Benchmark][date] = result
			}
		}
	}

	testKeys := []string{}
	for k := range results {
		testKeys = append(testKeys, k)
	}
	sort.Strings(testKeys)

	dateSet := map[time.Time]struct{}{}
	for _, k := range testKeys {
		for date := range results[k] {
			dateSet[date] = struct{}{}
		}
	}
	dateKeys := []time.Time{}
	for date := range dateSet {
		dateKeys = append(dateKeys, date)
	}
	sort.Slice(dateKeys, func(i, j int) bool {
		return dateKeys[i].Before(dateKeys[j])
	})

	w := csv.NewWriter(os.Stdout)
	w.Write(append([]string{"Date"}, testKeys...))
	for _, date := range dateKeys {
		row := []string{date.String()}
		for _, test := range testKeys {
			row = append(row, strconv.FormatFloat(results[test][date].Nanoseconds, 'f', -1, 64))
		}
		w.Write(row)
	}
	w.Flush()
}

type Result struct {
	Name        string
	Date        time.Time
	Nanoseconds float64
}
