package documentsview

import (
	"github.com/MaMissaoui/Finglass/internal/core/documents/service"
	"github.com/MaMissaoui/Finglass/internal/core/model"
)

// Clone returns a deep copy of the state.
//
// Rust's DocumentsState is moved into a worker and given back afterwards. Go
// copies the state before every background action, so the published snapshot and
// the worker's value never share a slice or a pointed-to field.
func (s *DocumentsState) Clone() *DocumentsState {
	if s == nil {
		return nil
	}
	cloned := *s
	cloned.Documents = cloneDocumentItems(s.Documents)
	cloned.SelectedDocument = cloneDocumentItem(s.SelectedDocument)
	cloned.SelectedOCRText = cloneString(s.SelectedOCRText)
	cloned.Suggestions = cloneSuggestions(s.Suggestions)
	cloned.LinkCandidates = cloneTransactions(s.LinkCandidates)
	cloned.Error = cloneString(s.Error)
	cloned.Message = cloneString(s.Message)
	return &cloned
}

func cloneDocumentItems(items []model.DocumentItem) []model.DocumentItem {
	if items == nil {
		return nil
	}
	cloned := make([]model.DocumentItem, len(items))
	for index := range items {
		cloned[index] = cloneDocumentItemValue(items[index])
	}
	return cloned
}

func cloneDocumentItem(item *model.DocumentItem) *model.DocumentItem {
	if item == nil {
		return nil
	}
	cloned := cloneDocumentItemValue(*item)
	return &cloned
}

func cloneDocumentItemValue(item model.DocumentItem) model.DocumentItem {
	cloned := item
	cloned.TransactionID = cloneTransactionID(item.TransactionID)
	cloned.TransactionLabel = cloneString(item.TransactionLabel)
	if item.OCRConfidence != nil {
		confidence := *item.OCRConfidence
		cloned.OCRConfidence = &confidence
	}
	return cloned
}

func cloneSuggestions(suggestions []service.MatchSuggestion) []service.MatchSuggestion {
	if suggestions == nil {
		return nil
	}
	cloned := make([]service.MatchSuggestion, len(suggestions))
	for index := range suggestions {
		cloned[index] = suggestions[index]
		cloned[index].Transaction = cloneTransactionValue(suggestions[index].Transaction)
	}
	return cloned
}

func cloneTransactions(items []model.TransactionItem) []model.TransactionItem {
	if items == nil {
		return nil
	}
	cloned := make([]model.TransactionItem, len(items))
	for index := range items {
		cloned[index] = cloneTransactionValue(items[index])
	}
	return cloned
}

func cloneTransactionValue(item model.TransactionItem) model.TransactionItem {
	cloned := item
	cloned.AccountIBAN = cloneString(item.AccountIBAN)
	cloned.PartnerName = cloneString(item.PartnerName)
	cloned.Purpose = cloneString(item.Purpose)
	cloned.CategoryID = cloneCategoryID(item.CategoryID)
	cloned.CategoryName = cloneString(item.CategoryName)
	cloned.CategoryColor = cloneString(item.CategoryColor)
	cloned.TaxRulePattern = cloneString(item.TaxRulePattern)
	if item.TaxOverride != nil {
		override := *item.TaxOverride
		cloned.TaxOverride = &override
	}
	if item.BalanceAfter != nil {
		balance := *item.BalanceAfter
		cloned.BalanceAfter = &balance
	}
	return cloned
}

func cloneTransactionID(value *model.TransactionID) *model.TransactionID {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneCategoryID(value *model.CategoryID) *model.CategoryID {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
