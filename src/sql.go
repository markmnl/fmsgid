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
	address = $1;`

const sqlInsertTx string = `insert into address_tx (address, ts, type, size) VALUES ($1, to_timestamp($2), $3, $4);`

const sqlActuals string = `with sent_totals as (
	select
		address
		, sum(size)	sent_size_total
	from
		address_tx
	where
		op = 'send'
	group by
		address
), sent_1d as (
	select
		address
		, count(*)	sent_count_1d
		, sum(size)	sent_size_1d
	from
		address_tx
	where
		op = 'send'
		and now() - ts < interval '1 day'
	group by
		address
), recv_totals as (
	select
		address
		, sum(size)	recv_size_total
	from
		address_tx
	where
		op = 'recv'
	group by
		address
), recv_1d as (
	select
		address
		, count(*)	recv_count_1d
		, sum(size)	recv_size_1d
	from
		address_tx
	where
		op = 'recv'
		and now() - ts < interval '1 day'
	group by
		address
)
select
	st.sent_size_total
	, sd.sent_count_1d
	, sd.sent_size_1d
	, rt.recv_size_total
	, rd.recv_count_1d
	, rd.recv_size_1d
from
	sent_totals st
	inner join sent_1d sd on st.address = sd.address
	inner join recv_totals rt on st.address = rt.address
	inner join recv_1d rd on st.address = rd.address
where
	st.address = $1;`
