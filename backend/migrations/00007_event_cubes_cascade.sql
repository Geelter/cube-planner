-- +goose Up
-- event_cubes.cube_id had no ON DELETE action, so deleting any cube linked
-- to an event surfaced a raw FK violation (opaque 500) — including someone
-- else's public cube the owner never consented to linking. The link simply
-- vanishes with the cube; version pins already tolerate the live-cube case.
alter table event_cubes
    drop constraint event_cubes_cube_id_fkey,
    add constraint event_cubes_cube_id_fkey
        foreign key (cube_id) references cubes (id) on delete cascade;

-- +goose Down
alter table event_cubes
    drop constraint event_cubes_cube_id_fkey,
    add constraint event_cubes_cube_id_fkey
        foreign key (cube_id) references cubes (id);
