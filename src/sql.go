package main

// sqlSelectAddressDetail selects an address with the limits of its quota pool
// (the pool owner's when quota_pool is set, else its own) and the pool's
// combined usage in a single query. The left join keeps the address's own
// limits should its pool owner be missing; quota_pool is the pool owner's
// address, NULL when the address isn't pooled. A release (types 3 and 4) takes
// a message deleted for good off the size totals, never below zero, but not off
// the daily usage.
const sqlSelectAddressDetail string = `select
	a.address
	, coalesce(a.display_name, '')
	, a.accepting_new
	, coalesce(o.limit_recv_size_total, a.limit_recv_size_total)
	, coalesce(o.limit_recv_size_per_msg, a.limit_recv_size_per_msg)
	, coalesce(o.limit_recv_size_per_1d, a.limit_recv_size_per_1d)
	, coalesce(o.limit_recv_count_per_1d, a.limit_recv_count_per_1d)
	, coalesce(o.limit_send_size_total, a.limit_send_size_total)
	, coalesce(o.limit_send_size_per_msg, a.limit_send_size_per_msg)
	, coalesce(o.limit_send_size_per_1d, a.limit_send_size_per_1d)
	, coalesce(o.limit_send_count_per_1d, a.limit_send_count_per_1d)
	, o.address as quota_pool
	, act.recv_size_total
	, act.recv_size_1d
	, act.recv_count_1d
	, act.send_size_total
	, act.send_size_1d
	, act.send_count_1d
from
	address a
	left join address o on o.address_lower = a.quota_pool
	cross join lateral (
		select
			greatest(0, coalesce(sum(size) filter (where type = 1), 0) - coalesce(sum(size) filter (where type = 3), 0)) as recv_size_total
			, coalesce(sum(size) filter (where type = 1 and ts > now() - interval '1 day'), 0) as recv_size_1d
			, count(*) filter (where type = 1 and ts > now() - interval '1 day') as recv_count_1d
			, greatest(0, coalesce(sum(size) filter (where type = 2), 0) - coalesce(sum(size) filter (where type = 4), 0)) as send_size_total
			, coalesce(sum(size) filter (where type = 2 and ts > now() - interval '1 day'), 0) as send_size_1d
			, count(*) filter (where type = 2 and ts > now() - interval '1 day') as send_count_1d
		from
			address_tx
		where
			address_lower = any (array(
				select address_lower
				from address
				where address_lower = coalesce(o.address_lower, a.address_lower)
					or quota_pool = coalesce(o.address_lower, a.address_lower)
			))
	) act
where
	a.address_lower = $1;`

const sqlInsertTx string = `insert into address_tx (address_lower, ts, type, size) VALUES ($1, to_timestamp($2), $3, $4);`

const sqlUpsertAddress string = `insert into address (
	address_lower, address, display_name, accepting_new,
	limit_recv_size_total, limit_recv_size_per_msg, limit_recv_size_per_1d, limit_recv_count_per_1d,
	limit_send_size_total, limit_send_size_per_msg, limit_send_size_per_1d, limit_send_count_per_1d
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
on conflict (address_lower) do update set
	address = excluded.address,
	display_name = excluded.display_name,
	accepting_new = excluded.accepting_new,
	limit_recv_size_total = excluded.limit_recv_size_total,
	limit_recv_size_per_msg = excluded.limit_recv_size_per_msg,
	limit_recv_size_per_1d = excluded.limit_recv_size_per_1d,
	limit_recv_count_per_1d = excluded.limit_recv_count_per_1d,
	limit_send_size_total = excluded.limit_send_size_total,
	limit_send_size_per_msg = excluded.limit_send_size_per_msg,
	limit_send_size_per_1d = excluded.limit_send_size_per_1d,
	limit_send_count_per_1d = excluded.limit_send_count_per_1d;`

// sqlDisableAbsentAddresses disables accepting addresses not present in the provided parameter array.
const sqlDisableAbsentAddresses string = `update address set accepting_new = false where accepting_new and address_lower != ALL($1);`

// sqlSelectAddressStateForUpdate reads an existing address's quota_pool and
// accepting_new, locking the row until the transaction ends.
const sqlSelectAddressStateForUpdate string = `select quota_pool, accepting_new from address where address_lower = $1 for update;`

// sqlSelectQuotaPoolForShare reads a prospective pool owner's own quota_pool,
// locking the row so it can't join another pool until the transaction ends.
const sqlSelectQuotaPoolForShare string = `select quota_pool from address where address_lower = $1 for share;`

// sqlIsPoolOwner reports whether any address is in the given address's pool.
const sqlIsPoolOwner string = `select exists (select 1 from address where quota_pool = $1 and address_lower <> $1);`
