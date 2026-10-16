//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type refundAttemptProviderTestDouble struct {
	refundProviderTestDouble
	refund func(context.Context, payment.RefundRequest) (*payment.RefundResponse, error)
	query  func(context.Context, payment.RefundQueryRequest) (*payment.RefundResponse, error)
}

func (p *refundAttemptProviderTestDouble) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	return p.refund(ctx, req)
}

func (p *refundAttemptProviderTestDouble) QueryRefund(ctx context.Context, req payment.RefundQueryRequest) (*payment.RefundResponse, error) {
	return p.query(ctx, req)
}

// Wallet changes use the same real SQLite transaction as order/audit changes.
// Failure injection writes first, so assertions also verify rollback, not only
// that a callback returned an error.
type refundAttemptSQLWallet struct {
	*mockUserRepo
	client         *dbent.Client
	failNextReturn atomic.Bool
	deductCalls    atomic.Int32
	returnCalls    atomic.Int32
}

func (w *refundAttemptSQLWallet) clientFor(ctx context.Context) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return w.client
}

func (w *refundAttemptSQLWallet) GetByID(ctx context.Context, id int64) (*User, error) {
	u, err := w.clientFor(ctx).User.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &User{ID: u.ID, Balance: u.Balance}, nil
}

func (w *refundAttemptSQLWallet) DeductAvailableBalance(ctx context.Context, id int64, amount float64) (float64, error) {
	w.deductCalls.Add(1)
	client := w.clientFor(ctx)
	u, err := client.User.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	deducted := math.Min(amount, math.Max(0, u.Balance))
	_, err = client.User.UpdateOneID(id).AddBalance(-deducted).Save(ctx)
	return deducted, err
}

func (w *refundAttemptSQLWallet) UpdateBalance(ctx context.Context, id int64, amount float64) error {
	if dbent.TxFromContext(ctx) == nil {
		return errors.New("refund repayment must join the order transaction")
	}
	w.returnCalls.Add(1)
	if _, err := w.clientFor(ctx).User.UpdateOneID(id).AddBalance(amount).Save(ctx); err != nil {
		return err
	}
	if w.failNextReturn.Swap(false) {
		return errors.New("injected failure after balance repayment")
	}
	return nil
}

func TestRefundRetryCarriesHeldDeductionAfterCompensationFailure(t *testing.T) {
	for _, tc := range []struct {
		name         string
		refundStatus string
		queryStatus  string
		gatewayError bool
		deduct       bool
		wantStatus   string
		wantBalance  float64
	}{
		{name: "pending then failed", refundStatus: payment.ProviderStatusPending, queryStatus: payment.ProviderStatusFailed, deduct: true, wantStatus: OrderStatusRefundFailed, wantBalance: 100},
		{name: "pending then success", refundStatus: payment.ProviderStatusPending, queryStatus: payment.ProviderStatusSuccess, deduct: true, wantStatus: OrderStatusRefunded, wantBalance: 40},
		{name: "pending without new deduction", refundStatus: payment.ProviderStatusPending, queryStatus: payment.ProviderStatusFailed, wantStatus: OrderStatusRefundFailed, wantBalance: 100},
		{name: "immediate success", refundStatus: payment.ProviderStatusSuccess, deduct: true, wantStatus: OrderStatusRefunded, wantBalance: 40},
		{name: "immediate gateway failure", gatewayError: true, deduct: true, wantStatus: OrderStatusCompleted, wantBalance: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createPendingRefundOrderWithDetailForTest(t, ctx, client, "retry-held",
				`{"refundID":"rf_test","deductionHeld":true,"deductionType":"balance","balanceDeducted":60}`)
			_, err := client.User.UpdateOneID(order.UserID).SetBalance(40).Save(ctx)
			require.NoError(t, err)
			wallet := &refundAttemptSQLWallet{mockUserRepo: &mockUserRepo{}, client: client}
			wallet.failNextReturn.Store(true)
			status := payment.ProviderStatusFailed
			prov := &refundAttemptProviderTestDouble{
				refund: func(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
					if tc.gatewayError {
						return nil, errors.New("gateway rejected refund")
					}
					return &payment.RefundResponse{RefundID: "rf_retry", Status: tc.refundStatus}, nil
				},
				query: func(context.Context, payment.RefundQueryRequest) (*payment.RefundResponse, error) {
					return &payment.RefundResponse{Status: status}, nil
				},
			}
			restore := replacePaymentProviderFactoryForTest(t, prov)
			defer restore()
			svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}, userRepo: wallet}

			result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.NoError(t, err)
			require.Contains(t, result.Warning, "rollback failed")
			user, err := client.User.Get(ctx, order.UserID)
			require.NoError(t, err)
			require.Equal(t, 40.0, user.Balance, "repayment SQL must roll back with the failed transaction")
			require.Zero(t, countRefundAuditsForTest(t, ctx, client, order.ID, "REFUND_DEDUCTION_RETURNED"))

			plan, early, err := svc.PrepareRefund(ctx, order.ID, 0, "retry", true, tc.deduct)
			require.NoError(t, err)
			require.Nil(t, early)
			require.True(t, plan.deductionHeld)
			require.Equal(t, 60.0, plan.BalanceToDeduct)
			result, err = svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			if tc.refundStatus == payment.ProviderStatusPending {
				detail, err := svc.latestRefundPendingDetail(ctx, order.ID)
				require.NoError(t, err)
				require.Equal(t, 60.0, detail.BalanceDeducted)
				status = tc.queryStatus
				result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantStatus == OrderStatusRefunded, result.Success)
			if result.Success {
				require.Equal(t, 60.0, result.BalanceDeducted)
			}
			require.Zero(t, wallet.deductCalls.Load(), "historical held balance must never be deducted again")
			user, err = client.User.Get(ctx, order.UserID)
			require.NoError(t, err)
			require.Equal(t, tc.wantBalance, user.Balance)
			current, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, current.Status)

			if tc.wantBalance == 100 {
				held, err := svc.outstandingRefundDeduction(ctx, order.ID)
				require.NoError(t, err)
				require.Nil(t, held, "successful repayment must retire the old failure marker")
				plan, early, err = svc.PrepareRefund(ctx, order.ID, 0, "fresh retry", false, true)
				require.NoError(t, err)
				require.Nil(t, early)
				require.False(t, plan.deductionHeld)
				require.Equal(t, 100.0, plan.BalanceToDeduct)
				prov.refund = func(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
					return &payment.RefundResponse{Status: payment.ProviderStatusSuccess}, nil
				}
				result, err = svc.ExecuteRefund(ctx, plan)
				require.NoError(t, err)
				require.True(t, result.Success)
				require.EqualValues(t, 1, wallet.deductCalls.Load(), "repaid historical deductions must not suppress a fresh deduction")
				user, err = client.User.Get(ctx, order.UserID)
				require.NoError(t, err)
				require.Zero(t, user.Balance)
			}
		})
	}
}

func TestRefundRetryCarriesLegacyHeldDeduction(t *testing.T) {
	for _, tc := range []struct {
		name          string
		orderType     string
		pendingDetail string
		failedDetail  string
	}{
		{
			name:          "balance",
			orderType:     payment.OrderTypeBalance,
			pendingDetail: `{"refundID":"rf_test","deductionHeld":true,"deductionType":"balance","balanceDeducted":60}`,
			failedDetail:  `{"balanceDeducted":60}`,
		},
		{
			name:          "subscription",
			orderType:     payment.OrderTypeSubscription,
			pendingDetail: `{"refundID":"rf_test","deductionHeld":true,"deductionType":"subscription","subDaysDeducted":7,"subscriptionID":42}`,
			failedDetail:  `{"subDaysDeducted":7}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createPendingRefundOrderWithDetailForTest(t, ctx, client, "legacy-held", tc.pendingDetail)
			_, err := client.PaymentOrder.UpdateOneID(order.ID).
				SetOrderType(tc.orderType).
				SetStatus(OrderStatusRefundFailed).
				Save(ctx)
			require.NoError(t, err)
			_, err = client.PaymentAuditLog.Create().
				SetOrderID(strconv.FormatInt(order.ID, 10)).
				SetAction("REFUND_ROLLBACK_FAILED").
				SetOperator("admin").
				SetDetail(tc.failedDetail).
				Save(ctx)
			require.NoError(t, err)
			restore := replacePaymentProviderFactoryForTest(t, &pendingRefundProviderTestDouble{queryStatus: payment.ProviderStatusSuccess})
			defer restore()
			// No wallet or subscription service: carrying held deductions must
			// neither inspect today's balance nor deduct subscription days again.
			svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 0, "retry legacy hold", true, true)
			require.NoError(t, err)
			require.Nil(t, early)
			require.True(t, plan.deductionHeld)
			result, err := svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			require.False(t, result.Success)
			detail, err := svc.latestRefundPendingDetail(ctx, order.ID)
			require.NoError(t, err)
			require.True(t, detail.DeductionHeld)
			if tc.orderType == payment.OrderTypeBalance {
				require.Equal(t, 60.0, detail.BalanceDeducted)
			} else {
				require.Equal(t, 7, detail.SubDaysDeducted)
				require.EqualValues(t, 42, detail.SubscriptionID)
			}
			result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.Equal(t, detail.BalanceDeducted, result.BalanceDeducted)
			require.Equal(t, detail.SubDaysDeducted, result.SubDaysDeducted)
		})
	}
}

func TestRefundRetryBindsAmountWhileDeductionHeld(t *testing.T) {
	for _, tc := range []struct {
		name         string
		refundAmount float64
		retryAmount  float64
	}{
		{name: "increase", refundAmount: 60, retryAmount: 100},
		{name: "decrease", refundAmount: 100, retryAmount: 60},
		{name: "same partial refund", refundAmount: 60, retryAmount: 60},
		{name: "same full refund", refundAmount: 100, retryAmount: 100},
	} {
		for _, force := range []bool{false, true} {
			for _, deduct := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/force=%t/deduct=%t", tc.name, force, deduct), func(t *testing.T) {
					ctx := context.Background()
					client := newPaymentConfigServiceTestClient(t)
					order := createCompletedRefundableOrderForTest(t, ctx, client, "retry-amount-change")
					_, err := client.User.UpdateOneID(order.UserID).SetBalance(100).Save(ctx)
					require.NoError(t, err)
					wallet := &refundAttemptSQLWallet{mockUserRepo: &mockUserRepo{}, client: client}
					wallet.failNextReturn.Store(true)
					refundCalls := 0
					queryStatus := payment.ProviderStatusFailed
					prov := &refundAttemptProviderTestDouble{
						refund: func(_ context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
							refundCalls++
							require.Equal(t, fmt.Sprintf("%.2f", tc.refundAmount), req.Amount)
							return &payment.RefundResponse{RefundID: "rf_original", Status: payment.ProviderStatusPending}, nil
						},
						query: func(context.Context, payment.RefundQueryRequest) (*payment.RefundResponse, error) {
							return &payment.RefundResponse{Status: queryStatus}, nil
						},
					}
					restore := replacePaymentProviderFactoryForTest(t, prov)
					defer restore()
					svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}, userRepo: wallet}

					// Run a real R1 for this amount, rather than holding 60 against
					// a historical forced refund of 100.
					plan, early, err := svc.PrepareRefund(ctx, order.ID, tc.refundAmount, "R1", false, true)
					require.NoError(t, err)
					require.Nil(t, early)
					result, err := svc.ExecuteRefund(ctx, plan)
					require.NoError(t, err)
					require.False(t, result.Success)
					result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
					require.NoError(t, err)
					require.Contains(t, result.Warning, "rollback failed")
					original, err := client.PaymentOrder.Get(ctx, order.ID)
					require.NoError(t, err)
					require.Equal(t, OrderStatusRefundFailed, original.Status)
					require.Equal(t, tc.refundAmount, original.RefundAmount)

					plan, early, err = svc.PrepareRefund(ctx, order.ID, tc.retryAmount, "R2", force, deduct)
					if tc.retryAmount == tc.refundAmount {
						require.NoError(t, err)
						require.Nil(t, early)
						require.True(t, plan.deductionHeld)
						require.Equal(t, tc.refundAmount, plan.RefundAmount)
						require.Equal(t, tc.refundAmount, plan.BalanceToDeduct)
						result, err = svc.ExecuteRefund(ctx, plan)
						require.NoError(t, err)
						require.False(t, result.Success)
						queryStatus = payment.ProviderStatusSuccess
						result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
						require.NoError(t, err)
						require.True(t, result.Success)
						require.Equal(t, tc.refundAmount, result.BalanceDeducted)
						require.Equal(t, 2, refundCalls)
						require.EqualValues(t, 1, wallet.deductCalls.Load())
						require.EqualValues(t, 1, wallet.returnCalls.Load())
						user, err := client.User.Get(ctx, order.UserID)
						require.NoError(t, err)
						require.Equal(t, 100-tc.refundAmount, user.Balance)
						current, err := client.PaymentOrder.Get(ctx, order.ID)
						require.NoError(t, err)
						require.Equal(t, tc.refundAmount, current.RefundAmount)
						wantStatus := OrderStatusRefunded
						if tc.refundAmount < order.Amount {
							wantStatus = OrderStatusPartiallyRefunded
						}
						require.Equal(t, wantStatus, current.Status)
						return
					}
					require.Error(t, err)
					require.Equal(t, "REFUND_AMOUNT_CHANGED", infraerrors.Reason(err))
					require.Nil(t, plan)
					require.Nil(t, early)
					require.Equal(t, 1, refundCalls, "changed R2 must never reach the provider")
					require.EqualValues(t, 1, wallet.deductCalls.Load())
					require.EqualValues(t, 1, wallet.returnCalls.Load())
					current, err := client.PaymentOrder.Get(ctx, order.ID)
					require.NoError(t, err)
					require.Equal(t, original.Status, current.Status)
					require.Equal(t, original.RefundAmount, current.RefundAmount)
					require.True(t, original.UpdatedAt.Equal(current.UpdatedAt))
					user, err := client.User.Get(ctx, order.UserID)
					require.NoError(t, err)
					require.Equal(t, 100-tc.refundAmount, user.Balance)
					held, err := svc.outstandingRefundDeduction(ctx, order.ID)
					require.NoError(t, err)
					require.NotNil(t, held)
					require.Equal(t, tc.refundAmount, held.RefundAmount)
					require.Equal(t, tc.refundAmount, held.BalanceDeducted)
					require.Equal(t, 1, countRefundAuditsForTest(t, ctx, client, order.ID, "REFUND_ROLLBACK_FAILED"))
					require.Zero(t, countRefundAuditsForTest(t, ctx, client, order.ID, "REFUND_DEDUCTION_RETURNED"))
				})
			}
		}
	}
}

func TestRefundRetryRestoresOriginalHeldRefundAmount(t *testing.T) {
	for _, tc := range []struct {
		name          string
		refundAmount  float64
		storedAmount  float64
		pendingDetail string
		failedDetail  string
		force         bool
		deduct        bool
		pendingAfter  bool
	}{
		{
			name: "original pending amount", refundAmount: 60, storedAmount: 100, deduct: true,
			pendingDetail: `{"refundID":"rf_test","refundAmount":60}`,
			failedDetail:  `{"balanceDeducted":60}`,
		},
		{
			name: "legacy order amount", refundAmount: 60, storedAmount: 60, deduct: true,
			pendingDetail: `{"refundID":"rf_test"}`,
			failedDetail:  `{"balanceDeducted":60}`,
		},
		{
			name: "failure amount precedes stale pending", refundAmount: 60, storedAmount: 100, deduct: true,
			pendingDetail: `{"refundID":"rf_stale","refundAmount":100}`,
			failedDetail:  `{"refundAmount":60,"deductionType":"balance","balanceDeducted":60}`,
		},
		{
			name: "same partial amount with force and no new deduction", refundAmount: 60, storedAmount: 60, force: true,
			pendingDetail: `{"refundID":"rf_test","refundAmount":60}`,
			failedDetail:  `{"balanceDeducted":60}`,
		},
		{
			name: "historical forced full refund with partial deduction", refundAmount: 100, storedAmount: 100, force: true, deduct: true,
			pendingDetail: `{"refundID":"rf_test"}`,
			failedDetail:  `{"balanceDeducted":60}`,
		},
		{
			name: "legacy failure precedes current pending", refundAmount: 60, storedAmount: 60, deduct: true,
			pendingDetail: `{"refundID":"rf_previous","refundAmount":100}`,
			failedDetail:  `{"balanceDeducted":60}`, pendingAfter: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createPendingRefundOrderWithDetailForTest(t, ctx, client, "retry-original-amount", tc.pendingDetail)
			_, err := client.PaymentOrder.UpdateOneID(order.ID).
				SetStatus(OrderStatusRefundFailed).
				SetRefundAmount(tc.storedAmount).
				Save(ctx)
			require.NoError(t, err)
			_, err = client.PaymentAuditLog.Create().
				SetOrderID(strconv.FormatInt(order.ID, 10)).
				SetAction("REFUND_ROLLBACK_FAILED").
				SetOperator("admin").
				SetDetail(tc.failedDetail).
				Save(ctx)
			require.NoError(t, err)
			if tc.pendingAfter {
				_, err = client.PaymentAuditLog.Create().
					SetOrderID(strconv.FormatInt(order.ID, 10)).
					SetAction("REFUND_PENDING").
					SetOperator("admin").
					SetDetail(`{"refundID":"rf_current","refundAmount":60,"deductionRollbackOK":false}`).
					Save(ctx)
				require.NoError(t, err)
				changed, early, err := (&PaymentService{entClient: client}).PrepareRefund(ctx, order.ID, 100, "stale previous amount", true, true)
				require.Equal(t, "REFUND_AMOUNT_CHANGED", infraerrors.Reason(err))
				require.Nil(t, changed)
				require.Nil(t, early)
			}
			refundCalls := 0
			prov := &refundAttemptProviderTestDouble{
				refund: func(_ context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
					refundCalls++
					require.Equal(t, fmt.Sprintf("%.2f", tc.refundAmount), req.Amount)
					return &payment.RefundResponse{RefundID: "rf_same_amount", Status: payment.ProviderStatusPending}, nil
				},
				query: func(_ context.Context, req payment.RefundQueryRequest) (*payment.RefundResponse, error) {
					require.Equal(t, fmt.Sprintf("%.2f", tc.refundAmount), req.Amount)
					return &payment.RefundResponse{Status: payment.ProviderStatusSuccess}, nil
				},
			}
			restore := replacePaymentProviderFactoryForTest(t, prov)
			defer restore()
			// A held retry must not fetch today's balance or deduct it again.
			svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}
			explicit, early, err := svc.PrepareRefund(ctx, order.ID, tc.refundAmount, "same amount", tc.force, tc.deduct)
			require.NoError(t, err)
			require.Nil(t, early)
			require.Equal(t, tc.refundAmount, explicit.RefundAmount)
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 0, "default to original amount", tc.force, tc.deduct)
			require.NoError(t, err)
			require.Nil(t, early)
			require.True(t, plan.deductionHeld)
			require.Equal(t, tc.refundAmount, plan.RefundAmount)
			require.Equal(t, tc.refundAmount, plan.GatewayAmount)
			require.Equal(t, 60.0, plan.BalanceToDeduct)
			result, err := svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			require.False(t, result.Success)
			detail, err := svc.latestRefundPendingDetail(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, tc.refundAmount, detail.RefundAmount)
			require.Equal(t, 60.0, detail.BalanceDeducted)
			result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.Equal(t, 60.0, result.BalanceDeducted)
			require.Equal(t, 1, refundCalls)
			current, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, tc.refundAmount, current.RefundAmount)
			wantStatus := OrderStatusRefunded
			if tc.refundAmount < order.Amount {
				wantStatus = OrderStatusPartiallyRefunded
			}
			require.Equal(t, wantStatus, current.Status)
		})
	}
}

func createLegacySubscriptionRefundForAttemptTest(t *testing.T, ctx context.Context, client *dbent.Client, suffix string) *dbent.PaymentOrder {
	t.Helper()
	// Real legacy pending/failure records have no subscriptionID or deductionType.
	order := createPendingRefundOrderWithDetailForTest(t, ctx, client, suffix,
		`{"refundID":"rf_legacy","refundAmount":60,"deductionRollbackOK":false,"subDaysDeducted":7}`)
	group, err := client.Group.Create().SetName(suffix + "-group").Save(ctx)
	require.NoError(t, err)
	order, err = client.PaymentOrder.UpdateOneID(order.ID).
		SetOrderType(payment.OrderTypeSubscription).
		SetSubscriptionGroupID(group.ID).
		SetSubscriptionDays(7).
		SetStatus(OrderStatusRefundFailed).
		SetRefundAmount(60).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(order.ID, 10)).
		SetAction("REFUND_ROLLBACK_FAILED").
		SetOperator("admin").
		SetDetail(`{"subDaysDeducted":7}`).
		Save(ctx)
	require.NoError(t, err)
	return order
}

func TestRefundRetryRestoresLegacySubscriptionBindingWithoutID(t *testing.T) {
	for _, state := range []string{"active", "expired", "revoked"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createLegacySubscriptionRefundForAttemptTest(t, ctx, client, "legacy-subscription")
			now := time.Now()
			create := client.UserSubscription.Create().
				SetUserID(order.UserID).
				SetGroupID(*order.SubscriptionGroupID).
				SetStartsAt(now.AddDate(0, 0, -30)).
				SetExpiresAt(now.AddDate(0, 0, 14))
			if state == "expired" {
				create.SetStatus(SubscriptionStatusExpired).SetExpiresAt(now.AddDate(0, 0, -1))
			}
			if state == "revoked" {
				create.SetDeletedAt(now.Add(-time.Hour))
			}
			original, err := create.Save(ctx)
			require.NoError(t, err)
			otherGroup, err := client.Group.Create().SetName("current-other-group").Save(ctx)
			require.NoError(t, err)
			current, err := client.UserSubscription.Create().
				SetUserID(order.UserID).
				SetGroupID(otherGroup.ID).
				SetStartsAt(now).
				SetExpiresAt(now.AddDate(0, 0, 30)).
				Save(ctx)
			require.NoError(t, err)
			otherUser, err := client.User.Create().
				SetEmail("other-subscription@example.com").
				SetPasswordHash("hash").
				SetUsername("other-subscription").
				Save(ctx)
			require.NoError(t, err)
			_, err = client.UserSubscription.Create().
				SetUserID(otherUser.ID).
				SetGroupID(*order.SubscriptionGroupID).
				SetStartsAt(now).
				SetExpiresAt(now.AddDate(0, 0, 30)).
				Save(ctx)
			require.NoError(t, err)
			prov := &pendingRefundProviderTestDouble{queryStatus: payment.ProviderStatusSuccess}
			restore := replacePaymentProviderFactoryForTest(t, prov)
			defer restore()
			svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}

			plan, early, err := svc.PrepareRefund(ctx, order.ID, 60, "legacy subscription retry", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			require.True(t, plan.deductionHeld)
			require.Equal(t, original.ID, plan.SubscriptionID)
			require.Equal(t, 7, plan.SubDaysToDeduct)
			result, err := svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			require.False(t, result.Success)
			detail, err := svc.latestRefundPendingDetail(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, original.ID, detail.SubscriptionID)
			require.Equal(t, 7, detail.SubDaysDeducted)
			require.Equal(t, 60.0, detail.RefundAmount)
			result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.Equal(t, 7, result.SubDaysDeducted)
			require.Equal(t, 1, prov.refundCalls)
			reloaded, err := client.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
			require.NoError(t, err)
			require.True(t, original.ExpiresAt.Equal(reloaded.ExpiresAt))
			require.Equal(t, original.Status, reloaded.Status)
			if original.DeletedAt == nil {
				require.Nil(t, reloaded.DeletedAt)
			} else {
				require.NotNil(t, reloaded.DeletedAt)
				require.True(t, original.DeletedAt.Equal(*reloaded.DeletedAt))
			}
			active, err := client.UserSubscription.Get(ctx, current.ID)
			require.NoError(t, err)
			require.True(t, current.ExpiresAt.Equal(active.ExpiresAt))
		})
	}
}

func TestRefundRetryRejectsMissingOrAmbiguousLegacySubscriptionBinding(t *testing.T) {
	for _, state := range []string{"missing subscription", "missing group", "revoked then replaced"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createLegacySubscriptionRefundForAttemptTest(t, ctx, client, "legacy-subscription-invalid")
			if state == "missing group" {
				_, err := client.PaymentOrder.UpdateOneID(order.ID).ClearSubscriptionGroupID().Save(ctx)
				require.NoError(t, err)
			}
			if state == "revoked then replaced" {
				now := time.Now()
				_, err := client.UserSubscription.Create().
					SetUserID(order.UserID).
					SetGroupID(*order.SubscriptionGroupID).
					SetStartsAt(now.AddDate(0, 0, -30)).
					SetExpiresAt(now.AddDate(0, 0, -1)).
					SetDeletedAt(now.Add(-time.Hour)).
					Save(ctx)
				require.NoError(t, err)
				_, err = client.UserSubscription.Create().
					SetUserID(order.UserID).
					SetGroupID(*order.SubscriptionGroupID).
					SetStartsAt(now).
					SetExpiresAt(now.AddDate(0, 0, 30)).
					Save(ctx)
				require.NoError(t, err)
			}
			svc := &PaymentService{entClient: client}
			for _, force := range []bool{false, true} {
				plan, early, err := svc.PrepareRefund(ctx, order.ID, 60, "ambiguous legacy retry", force, true)
				require.Error(t, err)
				require.Contains(t, err.Error(), "legacy refund subscription binding")
				require.Nil(t, plan)
				require.Nil(t, early)
			}
			current, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusRefundFailed, current.Status)
			require.Equal(t, 60.0, current.RefundAmount)
		})
	}
}

func TestRefundQueryCannotFinalizeLaterAttempt(t *testing.T) {
	for _, tc := range []struct {
		name           string
		lateStatus     string
		reuseTimestamp bool
	}{
		{name: "late failure", lateStatus: payment.ProviderStatusFailed},
		{name: "late success", lateStatus: payment.ProviderStatusSuccess},
		{name: "late failure with reused timestamp and refund id", lateStatus: payment.ProviderStatusFailed, reuseTimestamp: true},
		{name: "late success with reused timestamp and refund id", lateStatus: payment.ProviderStatusSuccess, reuseTimestamp: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newPaymentConfigServiceTestClient(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			order := createPendingRefundOrderWithDetailForTest(t, ctx, client, "attempt-race",
				`{"refundID":"rf_test","deductionHeld":true,"deductionType":"balance","balanceDeducted":100}`)
			wallet := &refundAttemptSQLWallet{mockUserRepo: &mockUserRepo{}, client: client}
			started := make(chan payment.RefundQueryRequest, 1)
			release := make(chan struct{})
			var releaseOnce sync.Once
			var workers sync.WaitGroup
			t.Cleanup(func() {
				cancel()
				releaseOnce.Do(func() { close(release) })
				workers.Wait()
			})
			var queryCalls atomic.Int32
			retryID := "rf_retry"
			if tc.reuseTimestamp {
				retryID = "rf_test"
			}
			prov := &refundAttemptProviderTestDouble{
				refund: func(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
					return &payment.RefundResponse{RefundID: retryID, Status: payment.ProviderStatusPending}, nil
				},
				query: func(ctx context.Context, req payment.RefundQueryRequest) (*payment.RefundResponse, error) {
					switch queryCalls.Add(1) {
					case 1:
						started <- req
						select {
						case <-release:
							return &payment.RefundResponse{RefundID: req.RefundID, Status: tc.lateStatus}, nil
						case <-ctx.Done():
							return nil, ctx.Err()
						}
					case 2:
						return &payment.RefundResponse{RefundID: req.RefundID, Status: payment.ProviderStatusFailed}, nil
					default:
						return &payment.RefundResponse{RefundID: req.RefundID, Status: payment.ProviderStatusSuccess}, nil
					}
				},
			}
			restore := replacePaymentProviderFactoryForTest(t, prov)
			defer restore()
			svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}, userRepo: wallet}
			type outcome struct {
				result *RefundResult
				err    error
			}
			done := make(chan outcome, 1)
			workers.Add(1)
			go func() {
				defer workers.Done()
				result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
				done <- outcome{result: result, err: err}
			}()
			select {
			case req := <-started:
				require.Equal(t, "rf_test", req.RefundID)
			case <-ctx.Done():
				t.Fatal("old refund query did not reach the barrier")
			}

			// R1 settles while its other query remains blocked in the provider.
			result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.NoError(t, err)
			require.False(t, result.Success)
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 0, "R2", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			result, err = svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			require.False(t, result.Success)
			if tc.reuseTimestamp {
				_, err := client.PaymentOrder.UpdateOneID(order.ID).SetUpdatedAt(order.UpdatedAt).Save(ctx)
				require.NoError(t, err)
			}
			r2, err := svc.latestRefundPendingDetail(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, retryID, r2.RefundID)

			releaseOnce.Do(func() { close(release) })
			select {
			case late := <-done:
				require.Nil(t, late.result)
				require.Equal(t, "CONFLICT", infraerrors.Reason(late.err))
			case <-ctx.Done():
				t.Fatal("old refund query did not finish")
			}
			current, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusRefundPending, current.Status)
			user, err := client.User.Get(ctx, order.UserID)
			require.NoError(t, err)
			require.Zero(t, user.Balance)
			require.EqualValues(t, 1, wallet.deductCalls.Load())
			require.EqualValues(t, 1, wallet.returnCalls.Load())
			require.Equal(t, 1, countRefundAuditsForTest(t, ctx, client, order.ID, "REFUND_FAILED"))
			require.Zero(t, countRefundAuditsForTest(t, ctx, client, order.ID, "REFUND_SUCCESS"))

			// R2 can still settle normally after the old result was rejected.
			result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.NoError(t, err)
			require.True(t, result.Success)
			require.Equal(t, 100.0, result.BalanceDeducted)
			require.EqualValues(t, 1, wallet.returnCalls.Load())
		})
	}
}
