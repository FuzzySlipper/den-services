-- Legacy rows imported from a text audience column may contain a singleton
-- string whose contents are themselves a JSON array (for example,
-- ["[\"all\"]"]). Safely unwrap only that unambiguous shape.
do $$
declare
    candidate record;
    parsed_audience jsonb;
begin
    for candidate in
        select id, audience ->> 0 as raw_audience
        from den_guidance.agent_guidance_entries
        where jsonb_typeof(audience) = 'array'
          and jsonb_array_length(audience) = 1
          and jsonb_typeof(audience -> 0) = 'string'
    loop
        begin
            parsed_audience := candidate.raw_audience::jsonb;
            if jsonb_typeof(parsed_audience) = 'array'
                and not exists (
                    select 1
                    from jsonb_array_elements(parsed_audience) as audience_values(value)
                    where jsonb_typeof(value) <> 'string'
                       or btrim(value #>> '{}') = ''
                )
            then
                update den_guidance.agent_guidance_entries
                set audience = nullif(parsed_audience, '[]'::jsonb)
                where id = candidate.id;
            end if;
        exception
            when others then
                -- Non-JSON and otherwise malformed legacy strings are a no-op.
                null;
        end;
    end loop;
end $$;
