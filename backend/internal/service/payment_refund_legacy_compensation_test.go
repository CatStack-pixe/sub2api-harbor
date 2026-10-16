//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

type refundRevokedSubscriptionRepo struct {
	userSubRepoNoop
	client           *dbent.Client
	failAfterRestore bool
	restoreCalls     int
}

func (r *refundRevokedSubscriptionRepo) clientFor(ctx context.Context) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return r.client
}

func refundSubscriptionState(row *dbent.UserSubscription) *UserSubscription {
	return &UserSubscription{
		ID: row.ID, UserID: row.UserID, GroupID: row.GroupID,
		Status: row.Status, StartsAt: row.StartsAt, ExpiresAt: row.ExpiresAt,
		DeletedAt: row.DeletedAt,
	}
}

func (r *refundRevokedSubscriptionRepo) GetByIDIncludeDeleted(ctx context.Context, id int64) (*UserSubscription, error) {
	row, err := r.clientFor(ctx).UserSubscription.Get(mixins.SkipSoftDelete(ctx), id)
	if err != nil {
		return nil, err
	}
	return refundSubscriptionState(row), nil
}

func (r *refundRevokedSubscriptionRepo) ExistsActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error) {
	return r.clientFor(ctx).UserSubscription.Query().
		Where(usersubscription.UserIDEQ(userID), usersubscription.GroupIDEQ(groupID), usersubscription.DeletedAtIsNil()).
		Exist(ctx)
}

func (r *refundRevokedSubscriptionRepo) Restore(ctx context.Context, id int64, status string) (*UserSubscription, error) {
	if dbent.TxFromContext(ctx) == nil {
		return nil, errors.New("subscription repayment must join the refund transaction")
	}
	r.restoreCalls++
	row, err := r.clientFor(ctx).UserSubscription.UpdateOneID(id).
		SetStatus(status).
		ClearDeletedAt().
		Save(mixins.SkipSoftDelete(ctx))
	if err != nil {
		return nil, err
	}
	if r.failAfterRestore {
		r.failAfterRestore = false
		return nil, errors.New("injected failure after subscription restoration")
	}
	return refundSubscriptionState(row), nil
}

func TestRefundRetryRepaysRevokedSubscriptionAtomically(t *testing.T) {
	for _, mode := range []string{"pending failure", "immediate failure", "restoration fails after write"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createLegacySubscriptionRefundForAttemptTest(t, ctx, client, "legacy-revoked-repayment")
			now := time.Now()
			original, err := client.UserSubscription.Create().
				SetUserID(order.UserID).
				SetGroupID(*order.SubscriptionGroupID).
				SetStartsAt(now.AddDate(0, 0, -30)).
				SetExpiresAt(now.AddDate(0, 0, 2)).
				SetDeletedAt(now.Add(-time.Hour)).
				Save(ctx)
			require.NoError(t, err)
			// Current-format audits prove that the deduction itself revoked this
			// row. A legacy row's deleted state alone does not prove that.
			detail, err := json.Marshal(refundPendingAuditDetail{
				RefundAmount: 60, DeductionType: payment.DeductionTypeSubscription,
				SubDaysDeducted: 7, SubscriptionID: original.ID, SubscriptionRevoked: true,
			})
			require.NoError(t, err)
			_, err = client.PaymentAuditLog.Create().
				SetOrderID(strconv.FormatInt(order.ID, 10)).
				SetAction("REFUND_ROLLBACK_FAILED").
				SetOperator("admin").
				SetDetail(string(detail)).
				Save(ctx)
			require.NoError(t, err)
			repo := &refundRevokedSubscriptionRepo{
				client: client, failAfterRestore: mode == "restoration fails after write",
			}
			prov := &refundAttemptProviderTestDouble{
				refund: func(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
					if mode == "immediate failure" {
						return nil, errors.New("gateway rejected retry")
					}
					return &payment.RefundResponse{RefundID: "rf_revoked", Status: payment.ProviderStatusPending}, nil
				},
				query: func(context.Context, payment.RefundQueryRequest) (*payment.RefundResponse, error) {
					return &payment.RefundResponse{Status: payment.ProviderStatusFailed}, nil
				},
			}
			restore := replacePaymentProviderFactoryForTest(t, prov)
			defer restore()
			svc := &PaymentService{
				entClient: client, loadBalancer: &captureLoadBalancer{},
				subscriptionSvc: &SubscriptionService{entClient: client, userSubRepo: repo},
			}
			plan, early, err := svc.PrepareRefund(ctx, order.ID, 60, "retry revoked subscription", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			require.True(t, plan.subscriptionRevoked)
			result, err := svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			require.False(t, result.Success)
			if mode != "immediate failure" {
				pending, err := svc.latestRefundPendingDetail(ctx, order.ID)
				require.NoError(t, err)
				require.True(t, pending.SubscriptionRevoked)
				result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
				require.NoError(t, err)
				require.False(t, result.Success)
			}
			reloaded, err := client.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
			require.NoError(t, err)
			require.True(t, original.ExpiresAt.Equal(reloaded.ExpiresAt), "revocation did not remove days from expiry")
			require.Equal(t, original.Status, reloaded.Status)
			require.Equal(t, 1, repo.restoreCalls)
			held, err := svc.outstandingRefundDeduction(ctx, order.ID)
			require.NoError(t, err)
			if mode == "restoration fails after write" {
				require.Contains(t, result.Warning, "rollback failed")
				require.NotNil(t, reloaded.DeletedAt, "restoration SQL must roll back with the failed refund transaction")
				require.NotNil(t, held)
				require.True(t, held.SubscriptionRevoked)
				require.Zero(t, countRefundAuditsForTest(t, ctx, client, order.ID, "REFUND_DEDUCTION_RETURNED"))
				plan, early, err = svc.PrepareRefund(ctx, order.ID, 60, "retry repayment", false, true)
				require.NoError(t, err)
				require.Nil(t, early)
				result, err = svc.ExecuteRefund(ctx, plan)
				require.NoError(t, err)
				require.False(t, result.Success)
				result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
				require.NoError(t, err)
				require.False(t, result.Success)
				held, err = svc.outstandingRefundDeduction(ctx, order.ID)
				require.NoError(t, err)
				require.Nil(t, held)
				reloaded, err = client.UserSubscription.Get(ctx, original.ID)
				require.NoError(t, err)
				require.Nil(t, reloaded.DeletedAt)
				require.True(t, original.ExpiresAt.Equal(reloaded.ExpiresAt))
				require.Equal(t, 2, repo.restoreCalls)
			} else {
				require.Nil(t, reloaded.DeletedAt)
				require.Nil(t, held)
			}
		})
	}
}

func TestRefundRetryRejectsUnprovenLegacyImmediateFailureAmount(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderWithDetailForTest(t, ctx, client, "unproven-immediate-amount",
		`{"refundID":"rf_previous","refundAmount":100,"deductionRollbackOK":true}`)
	order, err := client.PaymentOrder.UpdateOneID(order.ID).SetStatus(OrderStatusRefundFailed).Save(ctx)
	require.NoError(t, err)
	_, err = client.User.UpdateOneID(order.UserID).SetBalance(40).Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(order.ID, 10)).
		SetAction("REFUND_ROLLBACK_FAILED").
		SetOperator("admin").
		SetDetail(`{"balanceDeducted":60}`).
		Save(ctx)
	require.NoError(t, err)
	svc := &PaymentService{entClient: client}
	for _, amount := range []float64{0, 60, 100} {
		for _, force := range []bool{false, true} {
			plan, early, err := svc.PrepareRefund(ctx, order.ID, amount, "unproven original amount", force, true)
			require.ErrorContains(t, err, "legacy held refund amount requires reconciliation")
			require.Nil(t, plan)
			require.Nil(t, early)
		}
	}
	current, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, order.Status, current.Status)
	require.Equal(t, order.RefundAmount, current.RefundAmount)
	require.True(t, order.UpdatedAt.Equal(current.UpdatedAt))
	user, err := client.User.Get(ctx, order.UserID)
	require.NoError(t, err)
	require.Equal(t, 40.0, user.Balance)
	require.Zero(t, countRefundAuditsForTest(t, ctx, client, order.ID, "REFUND_DEDUCTION_RETURNED"))
}

func TestRefundRetryRejectsUnprovenLegacySubscriptionRevocation(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createLegacySubscriptionRefundForAttemptTest(t, ctx, client, "unproven-legacy-revocation")
	now := time.Now()
	// This row could have been shortened by seven days before an unrelated
	// administrator revocation. Restoring it alone would lose those seven days.
	original, err := client.UserSubscription.Create().
		SetUserID(order.UserID).
		SetGroupID(*order.SubscriptionGroupID).
		SetStartsAt(now.AddDate(0, 0, -30)).
		SetExpiresAt(now.AddDate(0, 0, 2)).
		SetDeletedAt(now.Add(-time.Hour)).
		Save(ctx)
	require.NoError(t, err)
	svc := &PaymentService{entClient: client}
	for _, force := range []bool{false, true} {
		plan, early, err := svc.PrepareRefund(ctx, order.ID, 60, "unproven legacy revocation", force, true)
		require.ErrorContains(t, err, "legacy revoked subscription deduction requires reconciliation")
		require.Nil(t, plan)
		require.Nil(t, early)
	}
	reloaded, err := client.UserSubscription.Get(mixins.SkipSoftDelete(ctx), original.ID)
	require.NoError(t, err)
	require.NotNil(t, reloaded.DeletedAt)
	require.True(t, original.ExpiresAt.Equal(reloaded.ExpiresAt))
	require.True(t, original.DeletedAt.Equal(*reloaded.DeletedAt))
	require.Zero(t, countRefundAuditsForTest(t, ctx, client, order.ID, "REFUND_DEDUCTION_RETURNED"))
}
