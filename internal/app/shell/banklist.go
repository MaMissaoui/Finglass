package shell

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/MaMissaoui/Finglass/internal/app/settingsview"
	"github.com/MaMissaoui/Finglass/internal/core/audit"
	"github.com/MaMissaoui/Finglass/internal/core/banklist"
	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/db"
	"github.com/MaMissaoui/Finglass/internal/core/i18n"
)

// reloadBankListLocked loads the installation-wide bank list. The caller holds
// the session mutex, or runs during Start before the session is shared.
func (s *Session) reloadBankListLocked() {
	list, loadedOn, present, err := banklist.Load(s.paths.Root())
	if err != nil {
		slog.Default().Warn("bank list could not be loaded", "error", err)
		return
	}
	s.bankList = list
	s.bankListLoadedOn = loadedOn
	s.bankListPresent = present
}

// BankListServerURL returns the loaded list's server address for blz.
func (s *Session) BankListServerURL(blz string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.bankList.Lookup(blz)
	if !ok {
		return "", false
	}
	return entry.URL, true
}

// SettingsLoadBankList asks for the DK list CSV and stores its server
// addresses in the data root.
//
// The path comes only from the native dialog, inside the job and without the
// session mutex. The list is installation-wide, so every profile sees it.
func (s *Session) SettingsLoadBankList() {
	s.mu.Lock()
	if !s.settingsActionReadyLocked() {
		s.mu.Unlock()
		return
	}
	dialogs := s.fileDialogs
	language := s.language
	root := s.paths.Root()
	today := s.today()
	var (
		loadedList  banklist.List
		loadedOn    civil.Date
		loadedReady bool
	)
	job := s.beginSettingsJobLocked(true, func(pending *settingsview.SettingsState) {
		pending.ClearMessages()
	}, func(
		ctx context.Context,
		database *db.Database,
		pending *settingsview.SettingsState,
	) {
		if dialogs == nil {
			return
		}
		path, ok := dialogs.PickBankList(i18n.Text(language, i18n.BankListLoad))
		if !ok || strings.TrimSpace(path) == "" {
			return
		}

		info, err := os.Stat(path)
		if err != nil {
			pending.Error = textPointer(language, i18n.ErrorUnexpected)
			return
		}
		if info.Size() > banklist.MaxBytes {
			pending.Error = textPointer(language, i18n.ErrorBankListTooLarge)
			return
		}
		// #nosec G304 -- the user chooses the bank list file.
		data, err := os.ReadFile(path)
		if err != nil {
			pending.Error = textPointer(language, i18n.ErrorUnexpected)
			return
		}
		list, report, err := banklist.Parse(data)
		if err != nil {
			message := bankListErrorText(language, err)
			pending.Error = &message
			return
		}
		if err := banklist.Save(root, list, today); err != nil {
			pending.Error = textPointer(language, i18n.ErrorUnexpected)
			return
		}
		if err := audit.Log(
			ctx,
			database,
			"BANK_LIST_LOADED",
			audit.ActorUser,
			fmt.Sprintf(
				"Bank list loaded: %d banks, %d rows skipped",
				report.Loaded,
				report.SkippedTotal(),
			),
		); err != nil {
			pending.Error = textPointer(language, i18n.ErrorUnexpected)
			return
		}

		loadedList = list
		loadedOn = today
		loadedReady = true
		pending.BankListPresent = true
		pending.BankListCount = report.Loaded
		pending.BankListLoadedOn = today
		message := bankListLoadedMessage(language, report)
		pending.Message = &message
	}, func(*settingsview.SettingsState) {
		if !loadedReady {
			return
		}
		s.bankList = loadedList
		s.bankListLoadedOn = loadedOn
		s.bankListPresent = true
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

// SettingsRemoveBankList deletes the installation-wide bank list.
func (s *Session) SettingsRemoveBankList() {
	s.mu.Lock()
	if !s.settingsActionReadyLocked() {
		s.mu.Unlock()
		return
	}
	root := s.paths.Root()
	language := s.language
	count := len(s.bankList)
	removed := false
	job := s.beginSettingsJobLocked(true, func(pending *settingsview.SettingsState) {
		pending.ClearMessages()
	}, func(
		ctx context.Context,
		database *db.Database,
		pending *settingsview.SettingsState,
	) {
		if err := banklist.Remove(root); err != nil {
			pending.Error = textPointer(language, i18n.ErrorUnexpected)
			return
		}
		if err := audit.Log(
			ctx,
			database,
			"BANK_LIST_REMOVED",
			audit.ActorUser,
			fmt.Sprintf("Bank list removed: %d banks", count),
		); err != nil {
			pending.Error = textPointer(language, i18n.ErrorUnexpected)
			return
		}
		removed = true
		pending.BankListPresent = false
		pending.BankListCount = 0
		pending.BankListLoadedOn = civil.Date{}
		message := i18n.Format(
			language,
			i18n.MessageBankListRemoved,
			i18n.Int(int64(count)),
		)
		pending.Message = &message
	}, func(*settingsview.SettingsState) {
		if !removed {
			return
		}
		s.bankList = nil
		s.bankListLoadedOn = civil.Date{}
		s.bankListPresent = false
	})
	s.mu.Unlock()
	s.startPageJob(job)
}

func textPointer(language i18n.Language, key i18n.Key) *string {
	message := i18n.Text(language, key)
	return &message
}

// bankListErrorText localizes the list's fixed errors.
func bankListErrorText(language i18n.Language, err error) string {
	switch {
	case errors.Is(err, banklist.ErrTooLarge):
		return i18n.Text(language, i18n.ErrorBankListTooLarge)
	case errors.Is(err, banklist.ErrUseCSV):
		return i18n.Text(language, i18n.ErrorBankListUseCsv)
	default:
		var missing *banklist.MissingColumnError
		if errors.As(err, &missing) {
			return i18n.Format(
				language,
				i18n.ErrorBankListMissingColumn,
				i18n.Str(missing.Column),
			)
		}
		return i18n.Text(language, i18n.ErrorUnexpected)
	}
}

// bankListLoadedMessage reports the loaded and skipped counts, with the skip
// reasons as a short list.
func bankListLoadedMessage(language i18n.Language, report banklist.Report) string {
	skipped := report.SkippedTotal()
	if skipped == 0 {
		return i18n.Format(
			language,
			i18n.MessageBankListLoadedAll,
			i18n.Int(int64(report.Loaded)),
		)
	}
	return i18n.Format(
		language,
		i18n.MessageBankListLoaded,
		i18n.Int(int64(report.Loaded)),
		i18n.Int(int64(skipped)),
		i18n.Str(bankListReasonsText(language, report.Skipped)),
	)
}

// bankListReasonsText renders each reason and its count, sorted by reason for a
// stable message.
func bankListReasonsText(language i18n.Language, skipped map[string]int) string {
	reasons := make([]string, 0, len(skipped))
	for reason := range skipped {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	parts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		if skipped[reason] == 0 {
			continue
		}
		parts = append(parts, i18n.Format(
			language,
			i18n.BankListSkippedReason,
			i18n.Str(bankListReasonLabel(language, reason)),
			i18n.Int(int64(skipped[reason])),
		))
	}
	return strings.Join(parts, ", ")
}

func bankListReasonLabel(language i18n.Language, reason string) string {
	switch reason {
	case "blz":
		return i18n.Text(language, i18n.BankListReasonBlz)
	case "not https":
		return i18n.Text(language, i18n.BankListReasonNotHTTPS)
	case "no scheme":
		return i18n.Text(language, i18n.BankListReasonNoScheme)
	case "empty":
		return i18n.Text(language, i18n.BankListReasonEmpty)
	case "duplicate":
		return i18n.Text(language, i18n.BankListReasonDuplicate)
	default:
		return i18n.Text(language, i18n.BankListReasonInvalid)
	}
}
