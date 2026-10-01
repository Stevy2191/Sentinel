-- 054_site_address_parts.sql
-- A site's address becomes a postal address block: street, city, state and
-- ZIP. The old one-line address is kept as the street; the user can move its
-- city, state and ZIP into their own fields.
ALTER TABLE sites RENAME COLUMN address TO street;
ALTER TABLE sites ADD COLUMN IF NOT EXISTS city TEXT;
ALTER TABLE sites ADD COLUMN IF NOT EXISTS state TEXT;
ALTER TABLE sites ADD COLUMN IF NOT EXISTS zip TEXT;
