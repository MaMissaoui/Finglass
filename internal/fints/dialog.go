// Ported from fints-rs 0.2.0 (MIT, © 2025 Florian Minnecker); see NOTICE.

package fints

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MaMissaoui/Finglass/internal/core/secret"
)

// DialogState is the closed state of a [Dialog].
type DialogState uint8

// The dialog states, mirroring fints-rs's Dialog<New|Synced|Open|TanPending>.
const (
	// DialogNew is a dialog that has not been synchronised or initialised.
	DialogNew DialogState = iota
	// DialogSynced is a dialog whose Sync succeeded and whose HKEND was sent.
	DialogSynced
	// DialogOpen is an authenticated dialog that may send business segments.
	DialogOpen
	// DialogTANPending is a dialog waiting for a TAN or app approval.
	DialogTANPending
	// DialogEnded is a dialog whose HKEND was sent, or that failed and was
	// closed. No further message is sent from it.
	DialogEnded
)

// String returns the state's name. It never contains bank data.
func (s DialogState) String() string {
	switch s {
	case DialogNew:
		return "new"
	case DialogSynced:
		return "synced"
	case DialogOpen:
		return "open"
	case DialogTANPending:
		return "tan-pending"
	case DialogEnded:
		return "ended"
	default:
		return "unknown"
	}
}

// DialogConfig is the fixed part of one bank login.
type DialogConfig struct {
	// BLZ is the German bank code.
	BLZ string
	// UserID is the online-banking login used in the signature and encryption
	// key names.
	UserID string
	// CustomerID is the Kunden-ID used in HKIDN.
	CustomerID string
	// SystemID is a remembered system id; Sync replaces it with the bank's.
	SystemID string
	// ProductID and ProductVersion travel in HKVVB. Both are supplied by the
	// caller; the package has no default.
	ProductID      string
	ProductVersion string
	// PIN is the caller-owned PIN. The dialog never copies or wipes it.
	PIN *secret.Text
	// Transport carries messages. It is required.
	Transport Transport
	// Sleeper waits between decoupled polls. A nil value uses a real sleeper
	// that honours ctx.
	Sleeper Sleeper
	// Source supplies the clock and security reference. Its zero value uses
	// the production defaults.
	Source Source
	// Params is optional remembered BPD/UPD state.
	Params *BankParams
	// PreferredSecurityFunction is the security function the caller remembered.
	PreferredSecurityFunction string
	// TANMedium is an optional TAN medium name.
	TANMedium string
}

// String returns only presence flags and counts. It never prints the user id,
// the customer id, the product id, the system id, the PIN or the TAN medium.
// Value receivers keep a printed DialogConfig value masked too.
func (c DialogConfig) String() string {
	return fmt.Sprintf(
		"fints.DialogConfig{blz:%d userID:%s customerID:%s systemID:%d productID:%s productVersion:%d pin:%t transport:%t sleeper:%t params:%t preferred:%s medium:%t}",
		len(c.BLZ), maskedPresence(c.UserID), maskedPresence(c.CustomerID), len(c.SystemID),
		maskedPresence(c.ProductID), len(c.ProductVersion), c.PIN != nil,
		c.Transport != nil, c.Sleeper != nil, c.Params != nil,
		c.PreferredSecurityFunction, c.TANMedium != "",
	)
}

// GoString returns only presence flags and counts.
func (c DialogConfig) GoString() string {
	return c.String()
}

// Format writes only presence flags and counts, for every fmt verb.
func (c DialogConfig) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(c.String()))
}

func maskedPresence(value string) string {
	if value == "" {
		return `""`
	}
	return "***"
}

// InitResult is the response-driven result of [Dialog.Init].
type InitResult struct {
	// Opened reports that the dialog is open and may send business segments.
	Opened bool
	// Challenge is the TAN challenge when Opened is false.
	Challenge TanChallenge
	// Decoupled reports that the challenge is confirmed in the bank's app.
	Decoupled bool
}

// String returns only the outcome and challenge lengths.
func (r InitResult) String() string {
	if r.Opened {
		return "fints.InitResult{opened:true}"
	}
	return fmt.Sprintf("fints.InitResult{opened:false challenge:%s decoupled:%t}", r.Challenge, r.Decoupled)
}

// GoString returns only the outcome and challenge lengths.
func (r InitResult) GoString() string {
	return r.String()
}

// Format writes only the outcome and challenge lengths, for every fmt verb.
func (r InitResult) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(r.String()))
}

// SendKind is the closed outcome of [Dialog.Send].
type SendKind uint8

// The Send outcomes.
const (
	// SendSuccess means the order was executed and the response holds its data.
	SendSuccess SendKind = iota
	// SendNeedTAN means the bank requires a TAN before it executes the order.
	SendNeedTAN
	// SendTouchdown means more pages are available at the touchdown point.
	SendTouchdown
)

// String returns the kind's name. It never contains bank data.
func (k SendKind) String() string {
	switch k {
	case SendSuccess:
		return "success"
	case SendNeedTAN:
		return "tan-required"
	case SendTouchdown:
		return "touchdown"
	default:
		return "unknown"
	}
}

// SendResult is the response-driven result of [Dialog.Send].
type SendResult struct {
	// Kind is the outcome.
	Kind SendKind
	// Response is the parsed bank response, when there is one.
	Response *Response
	// Challenge is the TAN challenge for SendNeedTAN.
	Challenge TanChallenge
	// Decoupled reports that the challenge is confirmed in the bank's app.
	Decoupled bool
	// Touchdown is the pagination point for SendTouchdown. It may carry bank
	// data and is never printed.
	Touchdown string
}

// String returns only the kind, the challenge lengths and the touchdown length.
func (r SendResult) String() string {
	return fmt.Sprintf(
		"fints.SendResult{kind:%s challenge:%s decoupled:%t touchdown:%d response:%s}",
		r.Kind, r.Challenge, r.Decoupled, len(r.Touchdown), responseDescription(r.Response),
	)
}

// GoString returns only the kind, the challenge lengths and the touchdown
// length.
func (r SendResult) GoString() string {
	return r.String()
}

// Format writes only the kind, the challenge lengths and the touchdown length,
// for every fmt verb.
func (r SendResult) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(r.String()))
}

// PollResult is the result of [Dialog.Poll]. Poll only returns after the bank
// confirmed the TAN; a still-pending answer is handled inside its bounded loop.
type PollResult struct {
	// Confirmed reports that the bank confirmed the TAN.
	Confirmed bool
	// Response is the confirming bank response.
	Response *Response
}

// String returns only the outcome and response counts.
func (r PollResult) String() string {
	return fmt.Sprintf("fints.PollResult{confirmed:%t response:%s}", r.Confirmed, responseDescription(r.Response))
}

// GoString returns only the outcome and response counts.
func (r PollResult) GoString() string {
	return r.String()
}

// Format writes only the outcome and response counts, for every fmt verb.
func (r PollResult) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(r.String()))
}

// Dialog is one FinTS dialog. Its state is checked on every transition; an
// invalid transition returns an [ErrorKindInvalidState] error and never sends a
// message.
//
// The dialog holds the caller's PIN pointer and never copies or wipes it. It
// stores no TAN: [Dialog.SubmitTAN] always wipes the TAN before returning.
type Dialog struct {
	blz            string
	userID         string
	customerID     string
	productID      string
	productVersion string
	pin            *secret.Text
	transport      Transport
	sleeper        Sleeper
	source         Source

	params     BankParams
	systemID   string
	dialogID   string
	messageNum uint16
	// dialogSecurityFunction is the security function the current dialog was
	// opened with; its HKEND is signed with the same one (999 for sync).
	dialogSecurityFunction string
	state                  DialogState
	challenge              TanChallenge
	decoupled              bool
}

// NewDialog builds a dialog in [DialogNew] state.
func NewDialog(config DialogConfig) (*Dialog, error) {
	if config.Transport == nil || config.PIN == nil {
		return nil, newInvalidStateError()
	}

	params := BankParams{}
	if config.Params != nil {
		params = cloneBankParams(*config.Params)
	} else {
		params = *NewBankParams()
	}
	if params.SelectedSecurityFunction == "" {
		params.SelectedSecurityFunction = SecurityFunctionPINOnly
	}
	if config.PreferredSecurityFunction != "" {
		params.PreferredSecurityFunction = config.PreferredSecurityFunction
	}
	if config.TANMedium != "" {
		params.SelectedTANMedium = config.TANMedium
	}

	systemID := config.SystemID
	if systemID == "" {
		systemID = params.SystemID
	}
	if systemID == "" {
		systemID = "0"
	}
	if params.SystemID == "" {
		params.SystemID = systemID
	}

	sleeper := config.Sleeper
	if sleeper == nil {
		sleeper = realSleeper{}
	}

	return &Dialog{
		blz:            config.BLZ,
		userID:         config.UserID,
		customerID:     config.CustomerID,
		productID:      config.ProductID,
		productVersion: config.ProductVersion,
		pin:            config.PIN,
		transport:      config.Transport,
		sleeper:        sleeper,
		source:         config.Source,
		params:         params,
		systemID:       systemID,
		messageNum:     1,
		state:          DialogNew,
	}, nil
}

func cloneBankParams(source BankParams) BankParams {
	clone := source
	clone.TANMethods = append([]TanMethod(nil), source.TANMethods...)
	clone.CamtFormats = append([]string(nil), source.CamtFormats...)
	clone.Accounts = append([]Account(nil), source.Accounts...)
	clone.AllowedSecurityFunctions = append([]string(nil), source.AllowedSecurityFunctions...)
	clone.bpdSegments = append([]ParsedSegment(nil), source.bpdSegments...)
	clone.updSegments = append([]ParsedSegment(nil), source.updSegments...)
	clone.OperationTANRequired = make(map[string]bool, len(source.OperationTANRequired))
	for operation, required := range source.OperationTANRequired {
		clone.OperationTANRequired[operation] = required
	}
	clone.SegmentVersions = make(map[BusinessSegment]uint16, len(source.SegmentVersions))
	for business, version := range source.SegmentVersions {
		clone.SegmentVersions[business] = version
	}
	return clone
}

// State returns the dialog state.
func (d *Dialog) State() DialogState {
	return d.state
}

// SystemID returns the system id assigned by the bank, or the remembered one.
func (d *Dialog) SystemID() string {
	return d.systemID
}

// DialogID returns the dialog id assigned by the bank, or "" before the first
// response or after HKEND.
func (d *Dialog) DialogID() string {
	return d.dialogID
}

// Params returns the dialog's bank parameters. The caller must not mutate them
// while the dialog is in use.
func (d *Dialog) Params() *BankParams {
	return &d.params
}

// Sync runs the synchronisation dialog: HKIDN (system id "0") + HKVVB + HKSYN
// with security function 999, then ingests the bank parameters and sends HKEND.
// It leaves the dialog in [DialogSynced] state, ready for Init.
func (d *Dialog) Sync(ctx context.Context) error {
	if d.state != DialogNew {
		return newInvalidStateError()
	}

	segments := []Segment{
		NewHKIDN(d.blz, d.customerID, "0"),
		NewHKVVB(d.params.BPDVersion, d.params.UPDVersion, d.productID, d.productVersion),
		NewHKSYN(),
	}
	response, err := d.exchange(ctx, SecurityFunctionPINOnly, nil, segments...)
	if err != nil {
		d.state = DialogEnded
		return err
	}

	d.applyResponse(response)
	d.params.SelectSecurityFunction()
	if dialogError := checkResponse(response); dialogError != nil {
		d.endAfterBankError(ctx, response, dialogError)
		return dialogError
	}

	// The sync dialog is always closed with HKEND, even before Init.
	d.endBestEffort(ctx)

	// Init starts a fresh dialog with its own id and message numbering.
	d.dialogID = ""
	d.messageNum = 1
	d.challenge = TanChallenge{}
	d.decoupled = false
	d.state = DialogSynced
	return nil
}

// Init opens the business dialog: HKIDN + HKVVB and HKTAN process 4 referencing
// HKIDN, as fints-rs's normal Init does. It returns Opened, or TanRequired with
// the challenge and whether the confirmation is decoupled.
func (d *Dialog) Init(ctx context.Context) (InitResult, error) {
	if d.state != DialogNew && d.state != DialogSynced {
		return InitResult{}, newInvalidStateError()
	}

	segments := []Segment{
		NewHKIDN(d.blz, d.customerID, d.systemID),
		NewHKVVB(d.params.BPDVersion, d.params.UPDVersion, d.productID, d.productVersion),
		NewHKTANProcess4(d.params.HKTANVersion(), TanRefHKIDN, d.tanMedium()),
	}
	response, err := d.exchange(ctx, d.params.SelectedSecurityFunction, nil, segments...)
	if err != nil {
		d.state = DialogEnded
		return InitResult{}, err
	}

	d.applyResponse(response)
	d.params.SelectSecurityFunction()
	if dialogError := checkInitResponse(response); dialogError != nil {
		d.endAfterBankError(ctx, response, dialogError)
		return InitResult{}, dialogError
	}

	if responseNeedsTAN(response) {
		challenge, ok := response.TANChallenge()
		if !ok {
			d.endBestEffort(ctx)
			return InitResult{}, newProtocolError()
		}
		d.challenge = challenge
		d.decoupled = responseDecoupled(response)
		d.state = DialogTANPending
		return InitResult{Challenge: challenge, Decoupled: d.decoupled}, nil
	}

	d.challenge = TanChallenge{}
	d.decoupled = false
	d.state = DialogOpen
	return InitResult{Opened: true}, nil
}

// Send sends one allow-listed business segment: HKSAL, HKKAZ, HKCAZ, HKSPA or
// HKTAB. It appends HKTAN process 4 referencing the segment when HIPINS says the
// operation needs a TAN. A Segment of any other type is rejected without a
// message.
func (d *Dialog) Send(ctx context.Context, segment Segment) (SendResult, error) {
	return d.send(ctx, segment, true)
}

// send is Send; withTAN false never appends HKTAN process 4. The workflow uses
// it for touchdown pages, where the strong authentication of the first page
// already covers the order (as in fints-rs).
func (d *Dialog) send(ctx context.Context, segment Segment, withTAN bool) (SendResult, error) {
	if d.state != DialogOpen {
		return SendResult{}, newInvalidStateError()
	}
	business, ok := businessForSegmentType(segment.segmentType())
	if !ok {
		return SendResult{}, newInvalidStateError()
	}
	if err := segment.validate(); err != nil {
		return SendResult{}, newProtocolError()
	}

	segments := []Segment{segment}
	if withTAN && d.params.NeedsTAN(business) {
		segments = append(segments, NewHKTANProcess4(
			d.params.HKTANVersion(),
			tanReferenceForBusiness(business),
			d.tanMedium(),
		))
	}

	response, err := d.exchange(ctx, d.params.SelectedSecurityFunction, nil, segments...)
	if err != nil {
		d.endBestEffort(ctx)
		return SendResult{}, err
	}

	d.applyResponse(response)
	if dialogError := checkResponse(response); dialogError != nil {
		d.endAfterBankError(ctx, response, dialogError)
		return SendResult{}, dialogError
	}

	// 3076 (strong authentication not required) wins over a TAN request, as
	// in fints-rs's balance and transaction calls.
	if responseNeedsTAN(response) && !response.HasKind(ReturnCodeKindScaExemption) {
		challenge, ok := response.TANChallenge()
		if !ok {
			d.endBestEffort(ctx)
			return SendResult{}, newProtocolError()
		}
		d.challenge = challenge
		d.decoupled = responseDecoupled(response)
		d.state = DialogTANPending
		return SendResult{
			Kind:      SendNeedTAN,
			Response:  response,
			Challenge: challenge,
			Decoupled: d.decoupled,
		}, nil
	}

	if point, ok := response.Touchdown(); ok {
		return SendResult{Kind: SendTouchdown, Response: response, Touchdown: point}, nil
	}

	return SendResult{Kind: SendSuccess, Response: response}, nil
}

// SubmitTAN sends HKTAN process 2 with the TAN. It always wipes tan before
// returning, on every path, and never stores it.
func (d *Dialog) SubmitTAN(ctx context.Context, tan *secret.Text) (*Response, error) {
	defer tan.Wipe()

	if d.state != DialogTANPending {
		return nil, newInvalidStateError()
	}
	if tan == nil || tan.IsEmpty() {
		return nil, newInvalidStateError()
	}

	segments := []Segment{
		NewHKTANProcess2(d.params.HKTANVersion(), d.challenge.TaskReference, d.tanMedium()),
	}
	response, err := d.exchange(ctx, d.params.SelectedSecurityFunction, tan, segments...)
	if err != nil {
		d.endBestEffort(ctx)
		return nil, err
	}

	d.applyResponse(response)
	if dialogError := checkResponse(response); dialogError != nil {
		d.endAfterBankError(ctx, response, dialogError)
		return nil, dialogError
	}
	if responseNeedsTAN(response) {
		d.endBestEffort(ctx)
		return nil, &DialogError{Kind: ErrorKindTanRejected}
	}

	d.challenge = TanChallenge{}
	d.decoupled = false
	d.state = DialogOpen
	return response, nil
}

// Poll sends HKTAN process S until the bank confirms the decoupled TAN or the
// selected method's poll limit is reached. It sleeps through the injected
// Sleeper before every poll, using the method's first and next wait values.
func (d *Dialog) Poll(ctx context.Context) (PollResult, error) {
	if d.state != DialogTANPending {
		return PollResult{}, newInvalidStateError()
	}

	firstWait, nextWait, maxPolls := d.pollTiming()
	for poll := 0; poll < maxPolls; poll++ {
		wait := firstWait
		if poll > 0 {
			wait = nextWait
		}
		if err := d.sleeper.Sleep(ctx, wait); err != nil {
			d.endBestEffort(ctx)
			if ctxErr := ctx.Err(); ctxErr != nil {
				return PollResult{}, newCancelledError(ctxErr)
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return PollResult{}, newCancelledError(err)
			}
			return PollResult{}, newTransportError(err)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			d.endBestEffort(ctx)
			return PollResult{}, newCancelledError(ctxErr)
		}

		segments := []Segment{
			NewHKTANProcessS(d.params.HKTANVersion(), d.challenge.TaskReference, d.tanMedium()),
		}
		response, err := d.exchange(ctx, d.params.SelectedSecurityFunction, nil, segments...)
		if err != nil {
			d.endBestEffort(ctx)
			return PollResult{}, err
		}
		d.applyResponse(response)
		if dialogError := checkResponse(response); dialogError != nil {
			d.endAfterBankError(ctx, response, dialogError)
			return PollResult{}, dialogError
		}
		if response.HasKind(ReturnCodeKindDecoupledPending) ||
			response.HasKind(ReturnCodeKindDecoupledInitiated) {
			continue
		}

		d.challenge = TanChallenge{}
		d.decoupled = false
		d.state = DialogOpen
		return PollResult{Confirmed: true, Response: response}, nil
	}

	d.endBestEffort(ctx)
	return PollResult{}, &DialogError{Kind: ErrorKindPollLimit}
}

// Cancel ends the dialog while a TAN is pending. It is equivalent to [End].
func (d *Dialog) Cancel(ctx context.Context) error {
	return d.End(ctx)
}

// End sends HKEND once a dialog id exists and marks the dialog ended. Its own
// error is ignored, as it is best effort. Calling End without a dialog id is a
// no-op that still marks the dialog ended.
func (d *Dialog) End(ctx context.Context) error {
	d.endBestEffort(ctx)
	return nil
}

// exchange builds, sends and parses one message. The request is wiped as soon
// as Exchange returns, including when Exchange panics.
func (d *Dialog) exchange(
	ctx context.Context,
	securityFunction string,
	tan *secret.Text,
	segments ...Segment,
) (*Response, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, newCancelledError(ctxErr)
	}
	if securityFunction == "" {
		securityFunction = SecurityFunctionPINOnly
	}

	input := MessageInput{
		BLZ:              d.blz,
		UserID:           d.userID,
		CustomerID:       d.customerID,
		SystemID:         d.systemID,
		DialogID:         d.dialogID,
		MessageNumber:    d.messageNum,
		SecurityFunction: securityFunction,
		PIN:              d.pin,
		TAN:              tan,
		Source:           d.source,
	}
	request, err := BuildMessage(input, segments...)
	if err != nil {
		return nil, newProtocolError()
	}
	if d.dialogID == "" || d.dialogID == "0" {
		d.dialogSecurityFunction = securityFunction
	}

	// The message number advances for every attempted message.
	d.messageNum++

	var raw []byte
	var transportErr error
	func() {
		defer Wipe(request)
		raw, transportErr = d.transport.Exchange(ctx, request)
	}()
	if transportErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, newCancelledError(ctxErr)
		}
		return nil, newTransportError(transportErr)
	}

	defer Wipe(raw)
	response, err := ParseResponse(raw)
	if err != nil {
		return nil, newProtocolError()
	}
	return response, nil
}

// endBestEffort sends HKEND when a dialog id exists, ignoring any error. The
// one exception is [Dialog.endAfterBankError]. The
// end message is built and wiped like every other message. A cancelled ctx does
// not suppress it.
func (d *Dialog) endBestEffort(ctx context.Context) {
	if d.dialogID != "" && d.dialogID != "0" {
		endContext := context.WithoutCancel(ctx)
		securityFunction := d.dialogSecurityFunction
		if securityFunction == "" {
			securityFunction = d.params.SelectedSecurityFunction
		}
		_, _ = d.exchange(endContext, securityFunction, nil, NewHKEND(d.dialogID))
	}
	d.dialogID = ""
	d.challenge = TanChallenge{}
	d.decoupled = false
	d.state = DialogEnded
}

// endAfterBankError ends the dialog after the bank rejected a message. HKEND
// is signed with the PIN, so after a PIN rejection or a locked account it is
// not sent: a second signed message could count against the bank's PIN
// failure limit. It is skipped too when the bank already aborted the dialog
// (9800). Otherwise HKEND goes out best effort, as on every error path.
func (d *Dialog) endAfterBankError(ctx context.Context, response *Response, dialogError *DialogError) {
	skip := dialogError != nil &&
		(dialogError.Kind == ErrorKindPinRejected || dialogError.Kind == ErrorKindAccountLocked)
	if response != nil && response.HasKind(ReturnCodeKindDialogAborted) {
		skip = true
	}
	if skip {
		d.dialogID = ""
		d.challenge = TanChallenge{}
		d.decoupled = false
		d.state = DialogEnded
		return
	}
	d.endBestEffort(ctx)
}

func (d *Dialog) applyResponse(response *Response) {
	d.extractDialogID(response)
	d.params.IngestResponse(response)
	if d.params.SystemID != "" {
		d.systemID = d.params.SystemID
	}
}

// extractDialogID adopts the dialog id from the first response's HNHBK and
// never overwrites it afterwards.
func (d *Dialog) extractDialogID(response *Response) {
	if response == nil {
		return
	}
	if d.dialogID != "" && d.dialogID != "0" {
		return
	}
	if dialogID := response.DialogID(); dialogID != "" && dialogID != "0" {
		d.dialogID = dialogID
	}
}

func (d *Dialog) tanMedium() *string {
	if d.params.SelectedTANMedium == "" {
		return nil
	}
	medium := d.params.SelectedTANMedium
	return &medium
}

// pollTiming returns the first wait, the next wait and the maximum number of
// polls. Absent values default to 5 s, 2 s and 60 polls; waits have a 1 s
// minimum and the poll count a hard cap of 120.
func (d *Dialog) pollTiming() (time.Duration, time.Duration, int) {
	firstSeconds := 5
	nextSeconds := 2
	maxPolls := 60
	if method, ok := d.params.SelectedMethod(); ok {
		if method.WaitBeforeFirstPoll > 0 {
			firstSeconds = method.WaitBeforeFirstPoll
		}
		if method.WaitBeforeNextPoll > 0 {
			nextSeconds = method.WaitBeforeNextPoll
		}
		if method.DecoupledMaxPolls > 0 {
			maxPolls = method.DecoupledMaxPolls
		}
	}
	if firstSeconds < 1 {
		firstSeconds = 1
	}
	if nextSeconds < 1 {
		nextSeconds = 1
	}
	if maxPolls > 120 {
		maxPolls = 120
	}
	return time.Duration(firstSeconds) * time.Second,
		time.Duration(nextSeconds) * time.Second,
		maxPolls
}

func responseDescription(response *Response) string {
	if response == nil {
		return "<nil>"
	}
	return response.String()
}

func responseNeedsTAN(response *Response) bool {
	if response == nil {
		return false
	}
	return response.HasKind(ReturnCodeKindTanRequired) ||
		response.HasKind(ReturnCodeKindDecoupledInitiated)
}

func responseDecoupled(response *Response) bool {
	return response != nil && response.HasKind(ReturnCodeKindDecoupledInitiated)
}

// businessForSegmentType maps the five sendable business segments to their
// closed BusinessSegment value.
func businessForSegmentType(segmentType string) (BusinessSegment, bool) {
	switch segmentType {
	case "HKSAL":
		return BusinessHKSAL, true
	case "HKKAZ":
		return BusinessHKKAZ, true
	case "HKCAZ":
		return BusinessHKCAZ, true
	case "HKSPA":
		return BusinessHKSPA, true
	case "HKTAB":
		return BusinessHKTAB, true
	default:
		return "", false
	}
}

func tanReferenceForBusiness(business BusinessSegment) TanSegmentRef {
	switch business {
	case BusinessHKSAL:
		return TanRefHKSAL
	case BusinessHKKAZ:
		return TanRefHKKAZ
	case BusinessHKCAZ:
		return TanRefHKCAZ
	case BusinessHKSPA:
		return TanRefHKSPA
	case BusinessHKTAB:
		return TanRefHKTAB
	default:
		return TanRefHKIDN
	}
}

// String returns only metadata and counts. It never prints the PIN, the dialog
// id, the system id or any bank text.
func (d Dialog) String() string {
	return fmt.Sprintf(
		"fints.Dialog{state:%s messageNumber:%d dialogID:%d systemID:%d params:%s challenge:%s decoupled:%t}",
		d.state, d.messageNum, len(d.dialogID), len(d.systemID), d.params.String(),
		d.challenge, d.decoupled,
	)
}

// GoString returns only metadata and counts, never a secret or bank text.
func (d Dialog) GoString() string {
	return d.String()
}

// Format writes only metadata and counts, for every fmt verb.
func (d Dialog) Format(f fmt.State, _ rune) {
	_, _ = f.Write([]byte(d.String()))
}
