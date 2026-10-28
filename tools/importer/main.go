// Command importer converts Poșta Română's official "Infocod" XLS file
// (data.gov.ro, OGL-ROU-1.0) into the gzipped CSV embedded by the server.
//
//	go run . -in infocod-cu-siruta-mai-2016.xls -out ../../data/postal.csv.gz
package main

import (
	"compress/gzip"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/shakinm/xlsReader/xls"
)

// Output columns, consumed by internal/store.
var header = []string{"county", "locality", "siruta", "street_type", "street", "number", "code", "sector"}

func main() {
	in := flag.String("in", "", "path to the Infocod .xls file")
	out := flag.String("out", "postal.csv.gz", "output .csv.gz path")
	flag.Parse()
	if *in == "" {
		flag.Usage()
		os.Exit(2)
	}

	wb, err := xls.OpenFile(*in)
	if err != nil {
		log.Fatalf("open %s: %v", *in, err)
	}

	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	gz, _ := gzip.NewWriterLevel(f, gzip.BestCompression)
	w := csv.NewWriter(gz)
	must(w.Write(header))

	total := 0
	for i := 0; i < wb.GetNumberSheets(); i++ {
		sheet, err := wb.GetSheet(i)
		if err != nil {
			log.Fatal(err)
		}
		cols := map[string]int{}
		rows := sheet.GetRows()
		if len(rows) == 0 {
			continue
		}
		for j, c := range rows[0].GetCols() {
			cols[strings.ToLower(strings.TrimSpace(c.GetString()))] = j
		}
		get := func(cells []string, name string) string {
			if j, ok := cols[name]; ok && j < len(cells) {
				return strings.TrimSpace(cells[j])
			}
			return ""
		}

		n := 0
		for _, row := range rows[1:] {
			var cells []string
			for _, c := range row.GetCols() {
				cells = append(cells, toUTF8(c.GetString()))
			}
			code := get(cells, "codpostal")
			if code == "" {
				continue
			}
			code = fmt.Sprintf("%06s", code)

			rec := []string{
				get(cells, "judet"),
				get(cells, "localitate"),
				get(cells, "siruta"),
				get(cells, "tip artera"),
				get(cells, "denumire artera"),
				get(cells, "numar"),
				code,
				get(cells, "sector"),
			}
			if rec[5] == "" {
				rec[5] = get(cells, "numar/bloc")
			}
			if _, isBuc := cols["sector"]; isBuc {
				// The Bucharest sheet has no county/locality columns.
				rec[0], rec[1] = "București", "București"
				rec[2] = get(cells, "siruta sector")
			}
			must(w.Write(rec))
			n++
		}
		log.Printf("sheet %q: %d rows", sheet.GetName(), n)
		total += n
	}

	w.Flush()
	must(w.Error())
	must(gz.Close())
	must(f.Close())
	log.Printf("wrote %d rows to %s", total, *out)
}

// toUTF8 fixes BIFF8 "compressed" strings, which the reader returns as raw
// Latin-1 bytes instead of UTF-8.
func toUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	r := make([]rune, len(s))
	for i := 0; i < len(s); i++ {
		r[i] = rune(s[i])
	}
	return string(r)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
