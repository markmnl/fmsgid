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

const sqlActuals string = `select
    sum(size) filter (where type = 2) as sent_size_total
    , count(*) filter (where type = 2 and ts > now() - interval '1 day') as sent_count_1d
    , sum(size) filter (where type = 2 and ts > now() - interval '1 day') as sent_size_1d
    , sum(size) filter (where type = 1) as recv_size_total
    , count(*) filter (where type = 1 and ts > now() - interval '1 day') as recv_count_1d
    , sum(size) filter (where type = 1 and ts > now() - interval '1 day') as recv_size_1d
from
	address_tx
where
	address_lower = $1;`
