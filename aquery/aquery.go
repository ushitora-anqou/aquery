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
	calltrace []string

	kind, desc                            map[string]struct{}
	count                                 int64
	minDuration, maxDuration, sumDuration int64 // in nanosecond
}

type stringBy []string

func (b stringBy) Len() int           { return len(b) }
func (b stringBy) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b stringBy) Less(i, j int) bool { return b[i] < b[j] }

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
	case "fullct":
		return strings.Join(ri.calltrace, "")
	case "fullct+kind":
		return strings.Join(append(ri.calltrace, ri.kind), "")
	case "topct+kind":
		return strings.Join([]string{ri.calltrace[0], ri.kind}, "")
	default: // topct
		return strings.Join([]string{ri.calltrace[0]}, "")
	}
}

func getDesc(ri rawInfo) string {
	switch ri.kind {
	case "Commit":
		return "COMMIT"
	case "Begin":
		return "BEGIN"
	case "Rollback":
		return "ROLLBACK"
	default:
		return ri.desc
	}
}

func main() {
	var (
		optGroupBy        = flag.String("group", "top", "Group by [topct|topct+kind|fullct|fullct+kind]")
		optSortBy         = flag.String("sort", "sum", "Sort by [count|min|max|sum|avg]")
		optCallstackRegex = flag.String("match-callstack", ".*", "Regex to match callstack with")
		optColWidth       = flag.Int("col", tablewriter.MAX_ROW_WIDTH, "Column width")
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
		desc := getDesc(*ri)
		if gi, ok := m[key]; ok {
			gi.count++
			gi.kind[ri.kind[0:2]] = struct{}{}
			if desc != "" {
				gi.desc[desc] = struct{}{}
			}

			gi.sumDuration += d
			if d < gi.minDuration {
				gi.minDuration = d
			}
			if gi.maxDuration < d {
				gi.maxDuration = d
			}
		} else {
			mKind := make(map[string]struct{})
			mDesc := make(map[string]struct{})
			mKind[ri.kind[0:2]] = struct{}{}
			if desc != "" {
				mDesc[desc] = struct{}{}
			}
			m[key] = &groupedInfo{
				kind:        mKind,
				desc:        mDesc,
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
	table.SetHeader([]string{"COUNT", "MIN", "MAX", "SUM", "AVG", "K", "CALLTRACE", "DESC"})
	table.SetColWidth(*optColWidth)
	for _, gi := range mSlice {
		// Format kind
		kind := []string{}
		for k := range gi.kind {
			kind = append(kind, k)
		}
		sort.Sort(stringBy(kind))

		// Format desc
		desc := []string{}
		for k := range gi.desc {
			desc = append(desc, k)
		}
		sort.Sort(stringBy(desc))

		// Format calltrace
		traces := []string{}
		switch *optGroupBy {
		case "fullct":
			for i, f := range gi.calltrace {
				traces = append(traces, fmt.Sprintf("%02d:%s", i, f))
			}
		case "fullct+kind":
			for i, f := range gi.calltrace {
				traces = append(traces, fmt.Sprintf("%02d:%s", i, f))
			}
		default: // topct, topct+kind
			traces = append(traces, gi.calltrace[0])
		}

		table.Append([]string{
			fmt.Sprint(gi.count),
			fmt.Sprintf("%.3f", float64(gi.minDuration)/1000000000.0),
			fmt.Sprintf("%.3f", float64(gi.maxDuration)/1000000000.0),
			fmt.Sprintf("%.3f", float64(gi.sumDuration)/1000000000.0),
			fmt.Sprintf("%.3f", float64(gi.sumDuration/gi.count)/1000000000.0),
			strings.Join(kind, ","),
			strings.Join(traces, "\n"),
			strings.Join(desc, "\n"),
		})
	}
	table.Render()
}
