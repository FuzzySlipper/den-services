alter table den_review.review_finalizations
    add column if not exists material_digest text;

comment on column den_review.review_finalizations.material_digest is
    'SHA-256 digest of the generated-time-free normalized reviewer result';
