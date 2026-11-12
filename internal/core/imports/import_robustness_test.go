package imports

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
)

var robustnessSamples = []struct {
	name string
	text string
}{
	{"a.sta", ":20:STARTUMSE\n:25:12030000/1234567890\n:28C:00001/001\n:60F:C260101EUR1000,00\n:61:2601020102DR12,34NMSCNONREF\n:86:105?00Lastschrift?20Einkauf Müller?32Müller GmbH\n:62F:C260102EUR987,66\n-\n"},
	{"a.ofx", "OFXHEADER:100\nDATA:OFXSGML\n<OFX><BANKMSGSRSV1><STMTTRNRS><STMTRS><CURDEF>EUR<BANKACCTFROM><ACCTID>DE89370400440532013000</BANKACCTFROM><BANKTRANLIST><STMTTRN><TRNTYPE>DEBIT<DTPOSTED>20260102<TRNAMT>-12.34<NAME>Müller<MEMO>Kauf</STMTTRN></BANKTRANLIST></STMTRS></STMTTRNRS></BANKMSGSRSV1></OFX>"},
	{"a.qif", "!Type:Bank\nD02/01/2026\nT-12.34\nPMüller\nMKauf\n^\nD03/01/26\nT5,00\n^\n"},
	{"a.csv", "Buchungsdatum;Wertstellung;Empfänger;Verwendungszweck;Betrag (€);Saldo\n02.01.2026;02.01.2026;Müller;Kauf;-12,34;987,66\n03.01.26;03.01.26;Bäcker;Brötchen;-1.234,50;1,00\n"},
	{"a.xml", "<?xml version=\"1.0\"?><Document xmlns=\"urn:iso:std:iso:20022:tech:xsd:camt.053.001.02\"><BkToCstmrStmt><Stmt><Acct><Id><IBAN>DE89370400440532013000</IBAN></Id></Acct><Ntry><Amt Ccy=\"EUR\">12.34</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts>BOOK</Sts><BookgDt><Dt>2026-01-02</Dt></BookgDt><NtryDtls><TxDtls><RltdPties><Cdtr><Nm>Müller</Nm></Cdtr></RltdPties><RmtInf><Ustrd>Kauf</Ustrd></RmtInf></TxDtls></NtryDtls></Ntry></Stmt></BkToCstmrStmt></Document>"},
}

func robustnessRun(name string, bytes []byte) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	today, _ := civil.New(2026, 11, 7)
	_, _ = Read(name, bytes, nil, today)
	return true
}

func TestNoParserPanics(t *testing.T) {
	panics := []string{}
	names := []string{"a.sta", "a.ofx", "a.qif", "a.csv", "a.xml", "a.xlsx", "a.bin"}
	for _, sample := range robustnessSamples {
		if !robustnessRun(sample.name, []byte(sample.text)) {
			panics = append(panics, sample.name+" sample")
		}
		runes := []rune(sample.text)
		for cut := 0; cut <= len(runes); cut++ {
			prefix := string(runes[:cut])
			if !robustnessRun(sample.name, []byte(prefix)) {
				panics = append(panics, fmt.Sprintf("%s prefix %d", sample.name, cut))
			}
			byteCut := min(cut, len(sample.text))
			if !robustnessRun(sample.name, []byte(sample.text)[:byteCut]) {
				panics = append(panics, fmt.Sprintf("%s byte-prefix %d", sample.name, cut))
			}
		}
		for position := 0; position < len(runes); position++ {
			mutated := append([]rune{}, runes[:position]...)
			mutated = append(mutated, 'ü')
			mutated = append(mutated, runes[position:]...)
			if !robustnessRun(sample.name, []byte(string(mutated))) {
				panics = append(panics, fmt.Sprintf("%s ü at %d", sample.name, position))
			}
		}
	}
	seed := uint64(42)
	for index := 0; index < 3000; index++ {
		length := index % 300
		bytes := make([]byte, length)
		for position := range bytes {
			seed = seed*6364136223846793005 + 1442695040888963407
			var buffer [8]byte
			binary.LittleEndian.PutUint64(buffer[:], seed>>33)
			bytes[position] = buffer[0]
		}
		name := names[index%len(names)]
		if !robustnessRun(name, bytes) {
			panics = append(panics, fmt.Sprintf("%s random #%d", name, index))
		}
	}
	if len(panics) > 0 {
		limit := min(len(panics), 15)
		t.Fatalf("%d panics, e.g. %v", len(panics), panics[:limit])
	}
}
