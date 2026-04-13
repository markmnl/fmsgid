package main

const sqlSelectAddressDetail string = `select
	address
	, display_name
	, accepting_new
	, limit_recv_size_total
	, limit_recv_size_per_msg
	, limit_recv_size_per_1d
	, limit_recv_count_per_1d
	, limit_send_size_total
	, limit_send_size_per_msg
	, limit_send_size_per_1d
	, limit_send_count_per_1d
from
	address
where
	address_lower = $1;`

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

// sqlDisableAbsentAddresses disables addresses not present in the provided parameter array.
const sqlDisableAbsentAddresses string = `update address set accepting_new = false where address_lower != ALL($1);`

const sqlActuals string = `select
	coalesce(sum(size) filter (where type = 2), 0) as sent_size_total
	, coalesce(sum(size) filter (where type = 2 and ts > now() - interval '1 day'), 0) as sent_size_1d
	, coalesce(sum(size) filter (where type = 1), 0) as recv_size_total
	, coalesce(sum(size) filter (where type = 1 and ts > now() - interval '1 day'), 0) as recv_size_1d
    , count(*) filter (where type = 2 and ts > now() - interval '1 day') as sent_count_1d
    , count(*) filter (where type = 1 and ts > now() - interval '1 day') as recv_count_1d
from
	address_tx
where
	address_lower = $1;`
