package governance

import store "github.com/pomkita/pomkita-be/internal/repository/store"

type Store = store.Store
type Decimal = store.Decimal

var NewDecimal = store.NewDecimal

type AckDecisionModel = store.AckDecisionModel
type AckHeadModel = store.AckHeadModel
type AckSupersessionModel = store.AckSupersessionModel
type AlertEventModel = store.AlertEventModel
type AlertRuleModel = store.AlertRuleModel
type AmendmentItemModel = store.AmendmentItemModel
type AmendmentModel = store.AmendmentModel
type DispenserReadingModel = store.DispenserReadingModel
type LossEntryModel = store.LossEntryModel
type SalesDeclaredModel = store.SalesDeclaredModel
type ShiftModel = store.ShiftModel
type ShiftReportModel = store.ShiftReportModel
type StationModel = store.StationModel
