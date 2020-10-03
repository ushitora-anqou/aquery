package main

import (
	"bufio"
	"compress/gzip"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
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

type stat struct {
	cnt                                        int
	min, max, sum, avg, dev, std, p1, p50, p99 float64
}

// Thanks to: https://github.com/tkuchiki/alp
func percentRank(l int, n int) int {
	pLen := (l * n / 100) - 1
	if pLen < 0 {
		pLen = 0
	}

	return pLen
}

func NewStat(src []float64) (s stat) {
	if len(src) == 0 {
		return
	}

	s.cnt = len(src)
	s.min, s.max = src[0], src[0]
	for _, v := range src {
		if v < s.min {
			s.min = v
		}
		if s.max < v {
			s.max = v
		}
		s.sum += v
	}
	s.avg = s.sum / float64(s.cnt)
	for _, v := range src {
		s.dev += (v - s.avg) * (v - s.avg)
	}
	s.dev = s.dev / float64(s.cnt)
	s.std = math.Sqrt(s.dev)

	tmp := make([]float64, s.cnt)
	copy(tmp, src)
	sort.Sort(float64By(tmp))
	s.p1 = tmp[percentRank(s.cnt, 1)]
	s.p50 = tmp[percentRank(s.cnt, 50)]
	s.p99 = tmp[percentRank(s.cnt, 99)]

	return
}

type groupedInfo struct {
	calltrace []string

	kind, desc   map[string]struct{}
	durations    []float64
	durationStat *stat
}

func nano2sec(src int64) float64 {
	return float64(src) / 1000000000.0
}

type stringBy []string

func (b stringBy) Len() int           { return len(b) }
func (b stringBy) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b stringBy) Less(i, j int) bool { return b[i] < b[j] }

type float64By []float64

func (b float64By) Len() int           { return len(b) }
func (b float64By) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b float64By) Less(i, j int) bool { return b[i] < b[j] }

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

func getKeyForGroupedInfoMap(ri rawInfo, opt map[string]struct{}) string {
	keys := []string{}

	if _, ok := opt["fullct"]; ok {
		keys = append(keys, ri.calltrace...)
	} else { // topct
		keys = append(keys, ri.calltrace[0])
	}

	if _, ok := opt["kind"]; ok {
		keys = append(keys, ri.kind)
	}

	if _, ok := opt["desc"]; ok {
		keys = append(keys, ri.desc)
	}

	return strings.Join(keys, ";")
}

func getShortFilePath(src string) string {
	return filepath.Base(src)
}

func main() {
	var (
		optGroupBySrc        = flag.String("group", "topct+desc", "Group by [topct|fullct]+[kind]+[desc]")
		optSortBy            = flag.String("sort", "sum", "Sort by [count|min|max|sum|avg]")
		optCalltraceRegex    = flag.String("match-ct", ".*", "Regex to match calltrace with")
		optInvCalltraceRegex = flag.String("inv-match-ct", "^$", "Regex to invertedly match calltrace with, that is, not-matching frames will be shown")
		optColWidth          = flag.Int("col", tablewriter.MAX_ROW_WIDTH, "Column width")
		optShortCalltrace    = flag.Bool("short-ct", false, "Show short file path for calltrace")
	)
	flag.Parse()
	if len(flag.Args()) != 1 {
		flag.Usage()
		os.Exit(1)
	}

	optGroupBy := make(map[string]struct{})
	for _, s := range strings.Split(*optGroupBySrc, "+") {
		s := strings.ToLower(s)
		switch s {
		case "topct":
			if _, ok := optGroupBy["fullct"]; ok {
				log.Fatalf("Invalid option: topct and fullct cannot be chosen at the same time")
			}
		case "fullct":
			if _, ok := optGroupBy["topct"]; ok {
				log.Fatalf("Invalid option: topct and fullct cannot be chosen at the same time")
			}
		}
		optGroupBy[s] = struct{}{}
	}

	// Open input file
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

	// Get raw data
	// input format: kind\000conn\000desc\000duration\000CSF0\000CSF1\000...CSFn\000\000
	raw := make([]*rawInfo, 0)
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
		switch kind {
		case "Commit":
			desc = "COMMIT"
		case "Begin":
			desc = "BEGIN"
		case "Rollback":
			desc = "ROLLBACK"
		}

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

	// Filter calltrace
	re := regexp.MustCompile(*optCalltraceRegex)
	reInv := regexp.MustCompile(*optInvCalltraceRegex)
	for _, ri := range raw {
		calltrace := make([]string, 0, len(ri.calltrace))
		for _, f := range ri.calltrace {
			if re.MatchString(f) && !reInv.MatchString(f) {
				calltrace = append(calltrace, f)
			}
		}
		ri.calltrace = calltrace
	}

	// Group by calltrace
	m := make(map[string]*groupedInfo)
	for _, ri := range raw {
		if len(ri.calltrace) == 0 {
			continue
		}
		key := getKeyForGroupedInfoMap(*ri, optGroupBy)
		d := ri.duration.Nanoseconds()
		if gi, ok := m[key]; ok {
			gi.kind[ri.kind[0:2]] = struct{}{}
			if ri.desc != "" {
				gi.desc[ri.desc] = struct{}{}
			}

			gi.durations = append(gi.durations, nano2sec(d))
		} else {
			mKind := make(map[string]struct{})
			mDesc := make(map[string]struct{})
			mKind[ri.kind[0:2]] = struct{}{}
			if ri.desc != "" {
				mDesc[ri.desc] = struct{}{}
			}
			m[key] = &groupedInfo{
				kind:      mKind,
				desc:      mDesc,
				calltrace: ri.calltrace,
				durations: []float64{nano2sec(d)},
			}
		}
	}

	// Calculate stat
	for _, gi := range m {
		s := NewStat(gi.durations)
		gi.durationStat = &s
	}

	// Sort
	mSlice := make([]*groupedInfo, 0, len(m))
	for _, gi := range m {
		mSlice = append(mSlice, gi)
	}
	by(func(gi1, gi2 *groupedInfo) bool {
		switch strings.ToLower(*optSortBy) {
		case "count":
			return gi1.durationStat.cnt > gi2.durationStat.cnt
		case "min":
			return gi1.durationStat.min > gi2.durationStat.min
		case "max":
			return gi1.durationStat.max > gi2.durationStat.max
		case "avg":
			return gi1.durationStat.avg > gi2.durationStat.avg
		case "std":
			return gi1.durationStat.std > gi2.durationStat.std
		case "p1":
			return gi1.durationStat.p1 > gi2.durationStat.p1
		case "p50":
			return gi1.durationStat.p50 > gi2.durationStat.p50
		case "p99":
			return gi1.durationStat.p99 > gi2.durationStat.p99
		default: // sum
			return gi1.durationStat.sum > gi2.durationStat.sum
		}
	}).Sort(mSlice)

	// Print
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"COUNT", "MIN", "MAX", "SUM", "AVG", "P1", "P50", "P99", "STD", "K", "CALLTRACE", "DESC"})
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
		if _, ok := optGroupBy["fullct"]; ok {
			for i, f := range gi.calltrace {
				if *optShortCalltrace {
					f = getShortFilePath(f)
				}
				traces = append(traces, fmt.Sprintf("%02d:%s", i, f))
			}
		} else {
			f := gi.calltrace[0]
			if *optShortCalltrace {
				f = getShortFilePath(f)
			}
			traces = append(traces, f)
		}

		s := gi.durationStat
		table.Append([]string{
			fmt.Sprint(s.cnt),
			fmt.Sprintf("%.3f", s.min),
			fmt.Sprintf("%.3f", s.max),
			fmt.Sprintf("%.3f", s.sum),
			fmt.Sprintf("%.3f", s.avg),
			fmt.Sprintf("%.3f", s.p1),
			fmt.Sprintf("%.3f", s.p50),
			fmt.Sprintf("%.3f", s.p99),
			fmt.Sprintf("%.3f", s.std),
			strings.Join(kind, ","),
			strings.Join(traces, "\n"),
			strings.Join(desc, "\n"),
		})
	}
	table.Render()
}
