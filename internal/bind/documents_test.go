package bind

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MaMissaoui/Finglass/internal/app/documentsview"
	"github.com/MaMissaoui/Finglass/internal/app/shell"
	"github.com/MaMissaoui/Finglass/internal/core/banksync"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/documents/service"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
	"github.com/MaMissaoui/Finglass/internal/core/model"
	"github.com/MaMissaoui/Finglass/internal/core/money"
	"github.com/MaMissaoui/Finglass/internal/core/paths"
)

func bindDocumentsItem(
	id int64,
	name string,
	pdf bool,
	linked *model.TransactionID,
	hasOCR bool,
) model.DocumentItem {
	item := model.DocumentItem{
		ID:         id,
		FileName:   name,
		StoredPath: "documents/" + name,
		UploadedAt: time.Unix(1_760_000_000, 0).UTC(),
		HasOCR:     hasOCR,
	}
	if pdf {
		item.MIMEType = "application/pdf"
	} else {
		item.MIMEType = "image/png"
	}
	if linked != nil {
		item.TransactionID = linked
		label := "Shop (2026-03-13)"
		item.TransactionLabel = &label
	}
	if hasOCR {
		confidence := 88.0
		item.OCRConfidence = &confidence
	}
	return item
}

func bindDocumentsState() documentsview.DocumentsState {
	linked := model.TransactionID(7)
	state := *documentsview.New(i18n.En, true)
	state.Documents = []model.DocumentItem{
		bindDocumentsItem(1, "bon.png", false, nil, false),
		bindDocumentsItem(2, "rechnung.pdf", true, &linked, true),
		bindDocumentsItem(3, "kassenbon.jpg", false, nil, true),
	}
	state.Counts = documentsview.DocumentCounts{All: 3, Unlinked: 2, Linked: 1}
	selected := state.Documents[2]
	state.SelectedDocument = &selected
	text := "SUMME 3,78"
	state.SelectedOCRText = &text
	return state
}

func TestDocumentRowsShowTheNameDateLinkStateAndOCRBADGE(t *testing.T) {
	dto := documentsDTO(shell.DocumentsPageState{
		State:       bindDocumentsState(),
		PreviewKind: "none",
	})
	rows := dto.Rows

	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if rows[0].Name != "bon.png" {
		t.Fatalf("name = %q", rows[0].Name)
	}
	if !strings.Contains(rows[0].Meta, "not linked") {
		t.Fatalf("meta = %q", rows[0].Meta)
	}
	if rows[0].IsPDF || rows[0].HasOCR {
		t.Fatalf("row 0 = %+v", rows[0])
	}
	if rows[1].Name != "rechnung.pdf" {
		t.Fatalf("name = %q", rows[1].Name)
	}
	if !strings.Contains(rows[1].Meta, "Shop (2026-03-13)") {
		t.Fatalf("meta = %q", rows[1].Meta)
	}
	if !rows[1].IsPDF || !rows[1].HasOCR || !rows[1].IsLinked {
		t.Fatalf("row 1 = %+v", rows[1])
	}
	if rows[0].Selected {
		t.Fatal("an unselected row is marked selected")
	}
	if !rows[2].Selected {
		t.Fatal("the state's selected document is not marked")
	}
}

func TestDocumentTabsCarryTheCountsOfEveryFilter(t *testing.T) {
	state := bindDocumentsState()
	dto := documentsDTO(shell.DocumentsPageState{State: state, PreviewKind: "none"})

	if dto.TabAllLabel != "All documents (3)" ||
		dto.TabUnlinkedLabel != "Unlinked (2)" ||
		dto.TabLinkedLabel != "Linked to transaction (1)" {
		t.Fatalf("labels = %q, %q, %q", dto.TabAllLabel, dto.TabUnlinkedLabel, dto.TabLinkedLabel)
	}
	if got := documentTabCode(state.Tab); got != "all" {
		t.Fatalf("tab = %q", got)
	}
	if got := documentTabFromCode("unlinked"); got != documentsview.DocumentTabUnlinked {
		t.Fatalf("unlinked tab = %v", got)
	}
	if got := documentTabFromCode("linked"); got != documentsview.DocumentTabLinked {
		t.Fatalf("linked tab = %v", got)
	}
	if got := documentTabFromCode("anything else"); got != documentsview.DocumentTabAll {
		t.Fatalf("unknown tab = %v", got)
	}
}

func TestDocumentMatchBadgesUseTheLanguagePercentConvention(t *testing.T) {
	if got := documentMatchBadge(i18n.En, 0.8); got != "match 80%" {
		t.Fatalf("En badge = %q", got)
	}
	if got := documentMatchBadge(i18n.De, 0.8); got != "Treffer 80\u00a0%" {
		t.Fatalf("De badge = %q", got)
	}
	if got := documentMatchBadge(i18n.Fr, 0.0); got != "correspondance 0\u00a0%" {
		t.Fatalf("Fr badge = %q", got)
	}
}

func TestDocumentOCRHeadingAndDeleteTextFollowTheSelection(t *testing.T) {
	state := bindDocumentsState()

	if got := documentOCRHeading(state); got != "Extracted text (confidence 88%)" {
		t.Fatalf("heading = %q", got)
	}
	if got := documentDeleteText(state); got != "kassenbon.jpg and its stored copy will be deleted." {
		t.Fatalf("delete text = %q", got)
	}

	empty := *documentsview.New(i18n.En, false)
	scan := bindDocumentsItem(4, "scan.pdf", true, nil, false)
	empty.SelectedDocument = &scan
	if got := documentOCRHeading(empty); got != "Extracted text" {
		t.Fatalf("empty heading = %q", got)
	}
	if len(empty.Documents) != 0 {
		t.Fatalf("empty documents = %d", len(empty.Documents))
	}
}

func TestDocumentDateAndMoneyTextMatchThePageFormat(t *testing.T) {
	date, ok := civil.New(2026, time.March, 13)
	if !ok {
		t.Fatal("invalid date")
	}
	if got := documentDateText(date); got != "13.03.2026" {
		t.Fatalf("date = %q", got)
	}
	if got := documentMoneyText(money.Money{Minor: -1234, Currency: money.EUR}, i18n.En); got != "-€12.34" {
		t.Fatalf("En money = %q", got)
	}
	usd, err := money.NewCurrency("USD")
	if err != nil {
		t.Fatalf("USD: %v", err)
	}
	if got := documentMoneyText(money.Money{Minor: 500, Currency: usd}, i18n.En); got != "$5.00" {
		t.Fatalf("USD money = %q", got)
	}
	if got := documentMoneyText(money.Money{Minor: -1234, Currency: money.EUR}, i18n.De); got != "-12,34\u00a0€" {
		t.Fatalf("De money = %q", got)
	}
}

func TestDocumentOCRHintNamesSettingsAndDataAndOCR(t *testing.T) {
	if got := documentOCRHint(i18n.En, true); got != "" {
		t.Fatalf("available hint = %q", got)
	}
	hint := documentOCRHint(i18n.En, false)
	if !strings.Contains(hint, "Tesseract") {
		t.Fatalf("hint = %q", hint)
	}
	if !strings.Contains(hint, "Settings → Data & OCR") {
		t.Fatalf("hint = %q", hint)
	}
}

func TestDocumentSuggestionRowsCarryTheBadge(t *testing.T) {
	partner := "Shop"
	date, ok := civil.New(2026, time.March, 13)
	if !ok {
		t.Fatal("invalid date")
	}
	rows := documentSuggestionRows([]service.MatchSuggestion{{
		Transaction: model.TransactionItem{
			ID:          model.TransactionID(7),
			Date:        date,
			Amount:      money.Money{Minor: -378, Currency: money.EUR},
			PartnerName: &partner,
		},
		Score: 0.8,
	}}, i18n.En)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Title != "Shop" || rows[0].Meta != "13.03.2026 · -€3.78" || rows[0].Badge != "match 80%" {
		t.Fatalf("row = %+v", rows[0])
	}
}

type documentsFixture struct {
	session *shell.Session
	service *Documents
	router  *Router
	events  chan DocumentsDTO
}

func newDocumentsFixture(t *testing.T) *documentsFixture {
	t.Helper()
	appPaths, err := paths.Resolve(t.TempDir(), "")
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	seedBindTransactions(t, appPaths)

	session, _, refusal := shell.Start(
		appPaths,
		banksync.NoCredentialStore{},
		nil,
		func() time.Time { return testNow },
		func() civil.Date { return civil.FromTime(testNow) },
		func(string) (string, bool) { return "", false },
	)
	if refusal != nil {
		t.Fatalf("start refused: %v", refusal)
	}
	t.Cleanup(func() { _ = session.Shutdown() })
	if err := session.Unlock([]byte(testPassphrase)); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	fixture := &documentsFixture{
		session: session,
		events:  make(chan DocumentsDTO, 256),
	}
	fixture.service = NewDocuments(session, DocumentsOptions{
		Emit: func(dto DocumentsDTO) {
			fixture.events <- dto
		},
	})
	fixture.router = NewRouter(session)
	fixture.router.Register("documents", fixture.service)
	t.Cleanup(func() {
		_ = fixture.router.Shutdown()
		_ = fixture.service.ServiceShutdown()
	})
	return fixture
}

func (f *documentsFixture) wait(t *testing.T, description string, predicate func(DocumentsDTO) bool) DocumentsDTO {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case dto := <-f.events:
			if predicate(dto) {
				return dto
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", description)
		}
	}
}

func TestDocumentsChangedEventEqualsTheReturnedDTO(t *testing.T) {
	fixture := newDocumentsFixture(t)

	returned, err := fixture.service.SetTab("unlinked")
	if err != nil {
		t.Fatalf("SetTab: %v", err)
	}
	event := fixture.wait(t, "the returned DTO's event", func(dto DocumentsDTO) bool {
		return reflect.DeepEqual(dto, returned)
	})
	if !reflect.DeepEqual(event, returned) {
		t.Fatalf("event = %+v, returned = %+v", event, returned)
	}

	final := fixture.wait(t, "the loaded unlinked tab", func(dto DocumentsDTO) bool {
		return !dto.Loading && dto.Tab == "unlinked"
	})
	state, err := fixture.service.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !reflect.DeepEqual(state, final) {
		t.Fatalf("State = %+v, last event = %+v", state, final)
	}
}

func TestDocumentsBindingsNeverTakeAPath(t *testing.T) {
	actualNames := documentsParameterNames(t)
	typ := reflect.TypeOf(&Documents{})
	methods := make(map[string]bool, typ.NumMethod())
	for index := 0; index < typ.NumMethod(); index++ {
		method := typ.Method(index)
		methods[method.Name] = true
		allowed, listed := allowedDocumentParameters[method.Name]
		names := actualNames[method.Name]
		if !listed && method.Type.NumIn() != 1 {
			t.Fatalf("%s takes arguments but is not in the allowed list", method.Name)
		}
		if len(names) != method.Type.NumIn()-1 {
			t.Fatalf("%s parameter names = %v, but reflection sees %d", method.Name, names, method.Type.NumIn()-1)
		}
		for _, name := range names {
			lower := strings.ToLower(name)
			for _, forbidden := range []string{"path", "file", "dir", "folder", "document", "content"} {
				if strings.Contains(lower, forbidden) {
					t.Fatalf("%s parameter %q could carry a path", method.Name, name)
				}
			}
		}
		if !listed {
			continue
		}
		if len(allowed) != len(names) {
			t.Fatalf("%s parameters = %v, want %v", method.Name, names, allowed)
		}
		for position, name := range allowed {
			if names[position] != name {
				t.Fatalf("%s parameter %d = %q, want %q", method.Name, position, names[position], name)
			}
			parameter := method.Type.In(position + 1)
			switch parameter.Kind() {
			case reflect.Int, reflect.Bool, reflect.String:
			default:
				t.Fatalf("%s parameter %q has type %s", method.Name, name, parameter)
			}
		}
	}

	for _, name := range []string{"Upload", "Open"} {
		if !methods[name] {
			t.Fatalf("%s is not exported", name)
		}
		if allowed, listed := allowedDocumentParameters[name]; listed && len(allowed) > 0 {
			t.Fatalf("%s must take no arguments", name)
		}
		if len(actualNames[name]) != 0 {
			t.Fatalf("%s takes %v", name, actualNames[name])
		}
	}
}

var allowedDocumentParameters = map[string][]string{
	"SetTab":                {"tab"},
	"Search":                {"query"},
	"Select":                {"id"},
	"CandidateQueryChanged": {"query"},
	"Link":                  {"transactionID"},
}

// documentsParameterNames reads the declared names from the service source.
// Reflection exposes a method's parameter types but not their names.
func documentsParameterNames(t *testing.T) map[string][]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "documents.go", nil, 0)
	if err != nil {
		t.Fatalf("parse documents.go: %v", err)
	}
	names := make(map[string][]string)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || len(function.Recv.List) == 0 {
			continue
		}
		if receiverType(function.Recv.List[0].Type) != "Documents" {
			continue
		}
		names[function.Name.Name] = parameterNamesOf(function.Type.Params)
	}
	return names
}

func TestAnEmptyDocumentsDTOSendsArraysNotNull(t *testing.T) {
	page := shell.DocumentsPageState{State: *documentsview.New(i18n.En, false), PreviewKind: "none"}
	encoded, err := json.Marshal(documentsDTO(page))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), ":null") {
		t.Fatalf("an empty Documents DTO has a null field: %s", encoded)
	}
}
