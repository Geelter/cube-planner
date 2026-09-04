-- +goose Up
-- 'removed' = organizer ejected the player. Distinct from 'cancelled'
-- (player withdrew) because a late Stripe payment may reclaim a
-- cancelled row but must never reinstate a removed one.
alter table registrations drop constraint registrations_status_check;
alter table registrations add constraint registrations_status_check
    check (status in (
        'pending_payment', 'paid', 'waitlisted',
        'cancelled', 'refund_requested', 'refunded', 'expired', 'removed'));

-- +goose Down
alter table registrations drop constraint registrations_status_check;
alter table registrations add constraint registrations_status_check
    check (status in (
        'pending_payment', 'paid', 'waitlisted',
        'cancelled', 'refund_requested', 'refunded', 'expired'));
