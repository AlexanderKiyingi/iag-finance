-- 071: Open and close fiscal periods by range
--
-- fiscal_periods (from the period-close work) is keyed 'YYYY-MM' and posting
-- into a closed period is blocked. Loading historical entries therefore means
-- opening every month the history spans and closing them again afterwards —
-- which up to now was a hand-written loop pasted into psql, with no record of
-- what it touched and nothing stopping it from silently reopening months that
-- were deliberately closed.
--
-- Both functions report a row per period describing what they did, so the
-- operator sees the effect before trusting it. Reopening a closed period is
-- possible but never implicit: it takes reopen_closed => true.

CREATE OR REPLACE FUNCTION open_fiscal_period_range(
    from_period   TEXT,
    to_period     TEXT,
    reopen_closed BOOLEAN DEFAULT FALSE
) RETURNS TABLE (period TEXT, action TEXT) AS $$
DECLARE
    cur      DATE;
    last     DATE;
    existing TEXT;
    label    TEXT;
    months   INT;
BEGIN
    IF from_period !~ '^\d{4}-(0[1-9]|1[0-2])$'
       OR to_period !~ '^\d{4}-(0[1-9]|1[0-2])$' THEN
        RAISE EXCEPTION 'fiscal periods must be YYYY-MM; got % and %',
            from_period, to_period;
    END IF;

    cur  := to_date(from_period, 'YYYY-MM');
    last := to_date(to_period,   'YYYY-MM');

    IF cur > last THEN
        RAISE EXCEPTION 'range runs backwards: % is after %', from_period, to_period;
    END IF;

    -- A mistyped year should fail loudly rather than insert centuries of rows.
    months := (EXTRACT(YEAR FROM last) - EXTRACT(YEAR FROM cur)) * 12
            + (EXTRACT(MONTH FROM last) - EXTRACT(MONTH FROM cur)) + 1;
    IF months > 600 THEN
        RAISE EXCEPTION 'range spans % months (% to %); refusing more than 600',
            months, from_period, to_period;
    END IF;

    WHILE cur <= last LOOP
        label := to_char(cur, 'YYYY-MM');
        SELECT fp.status INTO existing FROM fiscal_periods fp WHERE fp.period = label;

        IF existing IS NULL THEN
            INSERT INTO fiscal_periods (period, status) VALUES (label, 'open');
            period := label; action := 'created';
        ELSIF existing = 'open' THEN
            period := label; action := 'already open';
        ELSIF reopen_closed THEN
            UPDATE fiscal_periods fp
               SET status = 'open', closed_at = NULL, closed_by = NULL, updated_at = NOW()
             WHERE fp.period = label;
            period := label; action := 'reopened';
        ELSE
            period := label; action := 'left closed';
        END IF;

        RETURN NEXT;
        cur := (cur + INTERVAL '1 month')::DATE;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION close_fiscal_period_range(
    from_period TEXT,
    to_period   TEXT,
    actor       UUID DEFAULT NULL
) RETURNS TABLE (period TEXT, action TEXT) AS $$
DECLARE
    cur      DATE;
    last     DATE;
    existing TEXT;
    label    TEXT;
BEGIN
    IF from_period !~ '^\d{4}-(0[1-9]|1[0-2])$'
       OR to_period !~ '^\d{4}-(0[1-9]|1[0-2])$' THEN
        RAISE EXCEPTION 'fiscal periods must be YYYY-MM; got % and %',
            from_period, to_period;
    END IF;

    cur  := to_date(from_period, 'YYYY-MM');
    last := to_date(to_period,   'YYYY-MM');

    IF cur > last THEN
        RAISE EXCEPTION 'range runs backwards: % is after %', from_period, to_period;
    END IF;

    WHILE cur <= last LOOP
        label := to_char(cur, 'YYYY-MM');
        SELECT fp.status INTO existing FROM fiscal_periods fp WHERE fp.period = label;

        IF existing IS NULL THEN
            period := label; action := 'no such period';
        ELSIF existing = 'closed' THEN
            period := label; action := 'already closed';
        ELSE
            UPDATE fiscal_periods fp
               SET status = 'closed', closed_at = NOW(), closed_by = actor, updated_at = NOW()
             WHERE fp.period = label;
            period := label; action := 'closed';
        END IF;

        RETURN NEXT;
        cur := (cur + INTERVAL '1 month')::DATE;
    END LOOP;
END;
$$ LANGUAGE plpgsql;
