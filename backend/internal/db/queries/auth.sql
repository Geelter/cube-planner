-- name: CreateUser :one
insert into users (email, display_name, password_hash, email_verified_at)
values (lower(sqlc.arg(email)), sqlc.arg(display_name), sqlc.narg(password_hash), sqlc.narg(email_verified_at))
returning *;

-- name: GetUserByEmail :one
select * from users where email = lower(sqlc.arg(email));

-- name: GetUserByID :one
select * from users where id = sqlc.arg(id);

-- name: MarkEmailVerified :exec
update users set email_verified_at = now() where id = sqlc.arg(id) and email_verified_at is null;

-- name: SetPasswordHash :exec
update users set password_hash = sqlc.arg(password_hash) where id = sqlc.arg(id);

-- name: UpdateUnverifiedUser :one
update users set password_hash = sqlc.arg(password_hash), display_name = sqlc.arg(display_name)
where id = sqlc.arg(id) and email_verified_at is null
returning *;

-- name: CreateSession :exec
insert into sessions (token_hash, user_id, expires_at)
values (sqlc.arg(token_hash), sqlc.arg(user_id), sqlc.arg(expires_at));

-- name: GetSessionUserID :one
select user_id from sessions where token_hash = sqlc.arg(token_hash) and expires_at > now();

-- name: DeleteSession :exec
delete from sessions where token_hash = sqlc.arg(token_hash);

-- name: DeleteSessionsForUser :exec
delete from sessions where user_id = sqlc.arg(user_id);

-- name: CreateAuthToken :exec
insert into auth_tokens (token_hash, user_id, purpose, expires_at)
values (sqlc.arg(token_hash), sqlc.arg(user_id), sqlc.arg(purpose), sqlc.arg(expires_at));

-- name: ConsumeAuthToken :one
delete from auth_tokens
where token_hash = sqlc.arg(token_hash) and purpose = sqlc.arg(purpose) and expires_at > now()
returning user_id;

-- name: GetOAuthIdentity :one
select * from oauth_identities where provider = sqlc.arg(provider) and provider_user_id = sqlc.arg(provider_user_id);

-- name: CreateOAuthIdentity :exec
insert into oauth_identities (provider, provider_user_id, user_id)
values (sqlc.arg(provider), sqlc.arg(provider_user_id), sqlc.arg(user_id));

-- name: ListProvidersForUser :many
select provider from oauth_identities where user_id = sqlc.arg(user_id) order by provider;
