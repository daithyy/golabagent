with
    vector_hits as (
        select id, row_number() over (order by embedding <=> $1::vector) as rank
        from rag.chunks
        order by embedding <=> $1::vector
        limit $3
    ),
    keyword_terms as (
        select s.word, ln((select count(*) from rag.chunks)::float8 / s.ndoc) as idf
        from ts_stat('SELECT tsv FROM rag.chunks') s
        where s.word = any(tsvector_to_array(to_tsvector('english', $6)))
    ),
    keyword_hits as (
        select c.id, row_number() over (order by sum(t.idf) desc, c.id) as rank
        from rag.chunks c
        join keyword_terms t on t.word = any(tsvector_to_array(c.tsv))
        where c.tsv @@ to_tsquery('english', $2)
        group by c.id
        order by sum(t.idf) desc, c.id
        limit $3
    )
select
    c.source,
    c.chunk_index,
    c.content,
    coalesce(1.0 / ($5 + v.rank), 0) + coalesce(1.0 / ($5 + kw.rank), 0) as score,
    v.rank::int,
    kw.rank::int
from vector_hits v
full outer join keyword_hits kw using (id)
join rag.chunks c on c.id = coalesce(v.id, kw.id)
order by score desc
limit $4
