//! Robustness: no parser may panic on truncated, mangled or random input (added in review).
use finglass_core::import;
use time::macros::date;

const SAMPLES: &[(&str, &str)] = &[
    (
        "a.sta",
        ":20:STARTUMSE\n:25:12030000/1234567890\n:28C:00001/001\n:60F:C260101EUR1000,00\n:61:2601020102DR12,34NMSCNONREF\n:86:105?00Lastschrift?20Einkauf Müller?32Müller GmbH\n:62F:C260102EUR987,66\n-\n",
    ),
    (
        "a.ofx",
        "OFXHEADER:100\nDATA:OFXSGML\n<OFX><BANKMSGSRSV1><STMTTRNRS><STMTRS><CURDEF>EUR<BANKACCTFROM><ACCTID>DE89370400440532013000</BANKACCTFROM><BANKTRANLIST><STMTTRN><TRNTYPE>DEBIT<DTPOSTED>20260102<TRNAMT>-12.34<NAME>Müller<MEMO>Kauf</STMTTRN></BANKTRANLIST></STMTRS></STMTTRNRS></BANKMSGSRSV1></OFX>",
    ),
    (
        "a.qif",
        "!Type:Bank\nD02/01/2026\nT-12.34\nPMüller\nMKauf\n^\nD03/01/26\nT5,00\n^\n",
    ),
    (
        "a.csv",
        "Buchungsdatum;Wertstellung;Empfänger;Verwendungszweck;Betrag (€);Saldo\n02.01.2026;02.01.2026;Müller;Kauf;-12,34;987,66\n03.01.26;03.01.26;Bäcker;Brötchen;-1.234,50;1,00\n",
    ),
    (
        "a.xml",
        "<?xml version=\"1.0\"?><Document xmlns=\"urn:iso:std:iso:20022:tech:xsd:camt.053.001.02\"><BkToCstmrStmt><Stmt><Acct><Id><IBAN>DE89370400440532013000</IBAN></Id></Acct><Ntry><Amt Ccy=\"EUR\">12.34</Amt><CdtDbtInd>DBIT</CdtDbtInd><Sts>BOOK</Sts><BookgDt><Dt>2026-01-02</Dt></BookgDt><NtryDtls><TxDtls><RltdPties><Cdtr><Nm>Müller</Nm></Cdtr></RltdPties><RmtInf><Ustrd>Kauf</Ustrd></RmtInf></TxDtls></NtryDtls></Ntry></Stmt></BkToCstmrStmt></Document>",
    ),
];

fn run(name: &str, bytes: &[u8]) -> bool {
    let b = bytes.to_vec();
    let n = name.to_string();
    std::panic::catch_unwind(move || {
        let _ = import::read(&n, &b, None, date!(2026 - 11 - 07));
    })
    .is_ok()
}

#[test]
fn no_parser_panics() {
    std::panic::set_hook(Box::new(|_| {}));
    let mut panics = Vec::new();
    let mut seed: u64 = 42;
    let names = ["a.sta", "a.ofx", "a.qif", "a.csv", "a.xml", "a.xlsx", "a.bin"];
    for (name, text) in SAMPLES {
        assert!(run(name, text.as_bytes()));
        let chars: Vec<char> = text.chars().collect();
        for cut in 0..=chars.len() {
            let s: String = chars[..cut].iter().collect();
            if !run(name, s.as_bytes()) {
                panics.push(format!("{name} prefix {cut}"));
            }
            let raw = &text.as_bytes()[..cut.min(text.len())];
            if !run(name, raw) {
                panics.push(format!("{name} byte-prefix {cut}"));
            }
        }
        for pos in 0..chars.len() {
            let mut c = chars.clone();
            c.insert(pos, 'ü');
            let s: String = c.iter().collect();
            if !run(name, s.as_bytes()) {
                panics.push(format!("{name} ü at {pos}"));
            }
        }
    }
    for i in 0..3000 {
        let len = i % 300;
        let bytes: Vec<u8> = (0..len)
            .map(|_| {
                seed = seed.wrapping_mul(6364136223846793005).wrapping_add(1442695040888963407);
                (seed >> 33) as u8
            })
            .collect();
        let name = names[i % names.len()];
        if !run(name, &bytes) {
            panics.push(format!("{name} random #{i}"));
        }
    }
    panics.dedup();
    assert!(
        panics.is_empty(),
        "{} panics, e.g. {:?}",
        panics.len(),
        &panics[..panics.len().min(15)]
    );
}
