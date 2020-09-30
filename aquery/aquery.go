package main

import (
	"bufio"
	"compress/gzip"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
)

type rawInfo struct {
	kind, conn, desc string
	duration         time.Duration
	calltrace        []string
}

type groupedInfo struct {
	kind, desc string
	calltrace  []string

	count                                 int64
	minDuration, maxDuration, sumDuration int64 // in nanosecond
}

type by func(gi1, gi2 *groupedInfo) bool

func (b by) Sort(gis []*groupedInfo) {
	gs := &groupedInfoSorter{gis, b}
	sort.Sort(gs)
}

type groupedInfoSorter struct {
	gis []*groupedInfo
	by  func(gi1, gi2 *groupedInfo) bool
}

func (s *groupedInfoSorter) Len() int           { return len(s.gis) }
func (s *groupedInfoSorter) Swap(i, j int)      { s.gis[i], s.gis[j] = s.gis[j], s.gis[i] }
func (s *groupedInfoSorter) Less(i, j int) bool { return s.by(s.gis[i], s.gis[j]) }

func getKeyForGroupedInfoMap(ri rawInfo, opt string) string {
	switch opt {
	case "full":
		return strings.Join(append(ri.calltrace, ri.kind, ri.desc), "")
	default: // top
		return strings.Join([]string{ri.calltrace[0], ri.kind, ri.desc}, "")
	}
}

func main() {
	var (
		optGroupBy        = flag.String("group", "top", "Group by [top|full] of calltrace")
		optSortBy         = flag.String("sort", "sum", "Sort by [count|min|max|sum|avg]")
		optCallstackRegex = flag.String("match-callstack", ".*", "Regex to match callstack with")
	)
	flag.Parse()
	if len(flag.Args()) != 1 {
		flag.Usage()
		os.Exit(1)
	}

	inFileName := flag.Arg(0)
	infile, err := os.Open(inFileName)
	if err != nil {
		log.Fatalf("Could not open file \"%s\": %s", inFileName, err.Error())
	}
	zr, err := gzip.NewReader(infile)
	if err != nil {
		log.Fatalf("Could not open file \"%s\" as gzip: %s", inFileName, err.Error())
	}
	r := bufio.NewReader(zr)

	raw := make([]*rawInfo, 0)
	// format: kind\000conn\000desc\000duration\000CSF0\000CSF1\000...CSFn\000\000
	for {
		// kind
		kind, err := r.ReadString(0x00)
		if err != nil {
			break
		}
		kind = kind[:len(kind)-1] // Discard null

		// conn
		conn, err := r.ReadString(0x00)
		if err != nil {
			break
		}
		conn = conn[:len(conn)-1] // Discard null

		// desc
		desc, err := r.ReadString(0x00)
		if err != nil {
			break
		}
		desc = desc[:len(desc)-1] // Discard null

		// duration
		durationInNanoStr, err := r.ReadString(0x00)
		if err != nil {
			log.Fatalf("Invalid data: duration: %s", err.Error())
		}
		durationInNano, err := strconv.Atoi(durationInNanoStr[:len(durationInNanoStr)-1])
		if err != nil || durationInNano <= 0 {
			log.Fatalf("Invalid data: duration atoi: %s", err.Error())
		}
		duration := time.Duration(durationInNano)

		// call trace
		calltrace := make([]string, 0)
		for {
			file, err := r.ReadString(0x00)
			if err != nil || len(file) == 1 {
				break
			}
			file = file[:len(file)-1]

			linenoStr, err := r.ReadString(0x00)
			if err != nil {
				log.Fatalf("Invalid data: lineno: %s", err.Error())
			}
			lineno, err := strconv.Atoi(linenoStr[:len(linenoStr)-1])
			if err != nil {
				log.Fatalf("Invalid data: lineno atoi: %s", err.Error())
			}

			calltrace = append(calltrace, fmt.Sprintf("%s:%d", file, lineno))
		}

		// append
		raw = append(raw, &rawInfo{kind, conn, desc, duration, calltrace})
	}

	// Filter callstack
	re := regexp.MustCompile(*optCallstackRegex)
	for _, ri := range raw {
		calltrace := make([]string, 0, len(ri.calltrace))
		for _, f := range ri.calltrace {
			if re.MatchString(f) {
				calltrace = append(calltrace, f)
			}
		}
		ri.calltrace = calltrace
	}

	// Group by callstack
	m := make(map[string]*groupedInfo)
	for _, ri := range raw {
		key := getKeyForGroupedInfoMap(*ri, *optGroupBy)
		d := ri.duration.Nanoseconds()
		if gi, ok := m[key]; ok {
			// Assume gi.kind == ri.kind &&
			//        gi.desc == ri.desc && gi.calltrace == ri.calltrace
			gi.count++

			gi.sumDuration += d
			if d < gi.minDuration {
				gi.minDuration = d
			}
			if gi.maxDuration < d {
				gi.maxDuration = d
			}
		} else {
			m[key] = &groupedInfo{
				kind:        ri.kind,
				desc:        ri.desc,
				calltrace:   ri.calltrace,
				count:       1,
				sumDuration: d,
				minDuration: d,
				maxDuration: d,
			}
		}
	}

	// Sort
	mSlice := make([]*groupedInfo, 0, len(m))
	for _, gi := range m {
		mSlice = append(mSlice, gi)
	}
	by(func(gi1, gi2 *groupedInfo) bool {
		switch strings.ToLower(*optSortBy) {
		case "count":
			return gi1.count > gi2.count
		case "min":
			return gi1.minDuration > gi2.minDuration
		case "max":
			return gi1.maxDuration > gi2.maxDuration
		case "sum":
			return gi1.sumDuration > gi2.sumDuration
		case "avg":
			return gi1.sumDuration/gi1.count > gi2.sumDuration/gi2.count
		default: // sum
			return gi1.sumDuration > gi2.sumDuration
		}
	}).Sort(mSlice)

	// Print
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"COUNT", "MIN", "MAX", "SUM", "AVG", "K", "DESC", "CALLTRACE"})
	for _, gi := range mSlice {
		// Format calltrace
		traces := []string{}
		for i, f := range gi.calltrace {
			traces = append(traces, fmt.Sprintf("%02d:%s", i, f))
		}

		table.Append([]string{
			fmt.Sprint(gi.count),
			fmt.Sprintf("%.3f", float64(gi.minDuration)/1000000000.0),
			fmt.Sprintf("%.3f", float64(gi.maxDuration)/1000000000.0),
			fmt.Sprintf("%.3f", float64(gi.sumDuration)/1000000000.0),
			fmt.Sprintf("%.3f", float64(gi.sumDuration/gi.count)/1000000000.0),
			gi.kind[0:2],
			gi.desc,
			strings.Join(traces, "\n"),
		})
	}
	table.Render()
}
