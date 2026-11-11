// Package pdfdoc writes the small PDFs the exports need: a heading, flowing
// paragraphs and a table that breaks across pages and repeats its header.
//
// The writer embeds the IBM Plex Sans fonts the app already ships, so the euro
// sign, German umlauts, the sharp s and French accents stay in the text layer
// instead of being replaced by question marks.
package pdfdoc

import (
	"errors"
	"fmt"
	"strings"

	"github.com/signintech/gopdf"
)

const (
	// pageWidthPt and pageHeightPt are A4 in points.
	pageWidthPt  = 595.28
	pageHeightPt = 841.89
	// marginPt is the page margin on every side.
	marginPt = 50.0
	// footerSpacePt reserves room for the page footer.
	footerSpacePt = 18.0
	// bodySizePt and bodyLinePt size a paragraph line.
	bodySizePt = 10.0
	bodyLinePt = 14.0
	// headingSizePt is the heading font size.
	headingSizePt = 14.0
	// tableSizePt and tableRowPt size a table row.
	tableSizePt = 9.0
	tableRowPt  = 14.0
	// footerSizePt is the footer font size.
	footerSizePt = 8.0

	regularFont = "regular"
	boldFont    = "bold"
)

// Doc is a multi-page document under construction.
//
// The zero value is not usable; build one with [New].
type Doc struct {
	pdf      *gopdf.GoPdf
	y        float64
	finished bool
}

// New starts an A4 document with the regular and bold TrueType font data.
//
// The fonts are the IBM Plex Sans files the app bundles; no font is embedded in
// this package. The first page is created here.
func New(fontRegular, fontBold []byte) (*Doc, error) {
	if len(fontRegular) == 0 || len(fontBold) == 0 {
		return nil, errors.New("pdfdoc: the regular and bold font data are required")
	}

	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: pageWidthPt, H: pageHeightPt}})
	if err := pdf.AddTTFFontData(regularFont, fontRegular); err != nil {
		return nil, fmt.Errorf("pdfdoc: add regular font: %w", err)
	}
	if err := pdf.AddTTFFontData(boldFont, fontBold); err != nil {
		return nil, fmt.Errorf("pdfdoc: add bold font: %w", err)
	}
	pdf.AddPage()

	doc := &Doc{pdf: pdf}
	doc.reset()
	return doc, nil
}

// reset puts the cursor at the top margin of the current page.
func (d *Doc) reset() {
	d.y = marginPt
}

// contentWidth is the width between the left and right margins.
func contentWidth() float64 {
	return pageWidthPt - 2*marginPt
}

// contentBottom is the lowest baseline ordinary content may use.
func contentBottom() float64 {
	return pageHeightPt - marginPt - footerSpacePt
}

// ensureSpace starts a new page when height points would not fit below the
// current line.
func (d *Doc) ensureSpace(height float64) {
	if d.y+height > contentBottom() {
		d.pdf.AddPage()
		d.reset()
	}
}

// setFont selects one of the two embedded fonts.
func (d *Doc) setFont(family string, size float64) error {
	if err := d.pdf.SetFont(family, "", size); err != nil {
		return fmt.Errorf("pdfdoc: set font: %w", err)
	}
	return nil
}

// Heading writes a bold section heading.
func (d *Doc) Heading(text string) {
	d.ensureSpace(headingSizePt + 6)
	if err := d.setFont(boldFont, headingSizePt); err != nil {
		return
	}
	d.pdf.SetXY(marginPt, d.y)
	_ = d.pdf.Cell(&gopdf.Rect{W: contentWidth(), H: headingSizePt + 6}, text)
	d.y += headingSizePt + 8
}

// Paragraph writes flowing regular text. Lines break at word boundaries and
// continue on the next page when the current one is full.
func (d *Doc) Paragraph(text string) {
	if err := d.setFont(regularFont, bodySizePt); err != nil {
		return
	}

	for _, paragraphLine := range strings.Split(text, "\n") {
		lines := []string{paragraphLine}
		if paragraphLine != "" {
			wrapped, err := d.pdf.SplitTextWithWordWrap(paragraphLine, contentWidth())
			if err == nil {
				lines = wrapped
			}
		}

		for _, line := range lines {
			d.ensureSpace(bodyLinePt)
			d.pdf.SetXY(marginPt, d.y)
			_ = d.pdf.Cell(&gopdf.Rect{W: contentWidth(), H: bodyLinePt}, line)
			d.y += bodyLinePt
		}
	}
	d.y += 4
}

// Table writes a table. The header is bold and is repeated whenever the table
// continues on a new page. rightAligned marks the columns drawn to the right.
func (d *Doc) Table(header []string, rows [][]string, rightAligned []bool) {
	if len(header) == 0 {
		return
	}

	columns := len(header)
	columnWidth := contentWidth() / float64(columns)
	aligned := func(column int) int {
		if column < len(rightAligned) && rightAligned[column] {
			return gopdf.Right
		}
		return gopdf.Left
	}

	drawRow := func(cells []string, bold bool) {
		family := regularFont
		if bold {
			family = boldFont
		}
		if err := d.setFont(family, tableSizePt); err != nil {
			return
		}
		for column := 0; column < columns; column++ {
			value := ""
			if column < len(cells) {
				value = cells[column]
			}
			d.pdf.SetXY(marginPt+float64(column)*columnWidth, d.y)
			_ = d.pdf.CellWithOption(
				&gopdf.Rect{W: columnWidth, H: tableRowPt},
				value,
				gopdf.CellOption{Align: aligned(column)},
			)
		}
	}

	drawHeader := func() {
		d.ensureSpace(tableRowPt)
		drawRow(header, true)
		d.y += tableRowPt
	}

	drawHeader()
	for _, row := range rows {
		if d.y+tableRowPt > contentBottom() {
			d.pdf.AddPage()
			d.reset()
			drawHeader()
		}
		drawRow(row, false)
		d.y += tableRowPt
	}
}

// Bytes finishes the document and returns its bytes. Every page gets a
// right-aligned "n / N" footer.
func (d *Doc) Bytes() ([]byte, error) {
	if !d.finished {
		d.addFooters()
		d.finished = true
	}
	data, err := d.pdf.GetBytesPdfReturnErr()
	if err != nil {
		return nil, fmt.Errorf("pdfdoc: finish document: %w", err)
	}
	return data, nil
}

// addFooters draws "n / N" at the bottom right of every page.
func (d *Doc) addFooters() {
	total := d.pdf.GetNumberOfPages()
	if total == 0 {
		return
	}

	for page := 1; page <= total; page++ {
		if err := d.pdf.SetPage(page); err != nil {
			return
		}
		if err := d.setFont(regularFont, footerSizePt); err != nil {
			return
		}
		label := fmt.Sprintf("%d / %d", page, total)
		width, err := d.pdf.MeasureTextWidth(label)
		if err != nil {
			width = 0
		}
		d.pdf.SetXY(pageWidthPt-marginPt-width, pageHeightPt-marginPt)
		_ = d.pdf.Cell(&gopdf.Rect{W: width + 2, H: footerSizePt + 2}, label)
	}
}
