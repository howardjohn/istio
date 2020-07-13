package main

import (
	"flag"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	"golang.org/x/perf/benchstat"
)

var (
	commitDates = flag.String("commit-dates", "", "file containing `git log --format=\"format:%H,%cD\" --date-order`")
)

const rfc2822 = "Mon, 2 Jan 2006 15:04:05 -0700"
func main() {
	flag.Parse()

	if commitDates == nil {
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

	for _, file := range flag.Args() {
		f, err := os.Open(file)
		if err != nil {
			log.Fatal(err)
		}
		if err := c.AddFile(file, f); err != nil {
			log.Fatal(err)
		}
		f.Close()
	}

	for _, t := range c.Tables() {

		log.Println(t.Metric)
		log.Println(t.Configs)
		for _, row := range t.Rows {
			log.Printf("%+v\n", row.Metrics[0].FormatDiff())
		}
	}
}
