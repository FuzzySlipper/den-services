alter table den_review.review_rounds
    add column target_kind text not null default 'code_diff',
    add column campaign_children jsonb not null default '[]'::jsonb,
    add column campaign_repositories jsonb not null default '[]'::jsonb;

alter table den_review.review_rounds
    alter column branch drop not null,
    alter column base_branch drop not null,
    alter column base_commit drop not null,
    alter column head_commit drop not null;

comment on column den_review.review_rounds.target_kind is
  'Typed review authority: code_diff or campaign_reconciliation.';
comment on column den_review.review_rounds.campaign_children is
  'Immutable campaign child task and approved review-round snapshot.';
comment on column den_review.review_rounds.campaign_repositories is
  'Immutable exact repository/head tuples for campaign reconciliation.';
