-- ---------------------------------------------------------------------------- --
-- Seed data.
--
-- Covers every case the brief lists, plus the class the last seat race script
-- needs. Names are obviously fake and every address uses example.test, so no
-- real person appears in a screen recording.
--
-- Identifiers are fixed rather than generated, so a test, a script, and the
-- video can all name the same row.
--
-- Applied by backend/scripts/seed.sh, never by backend/scripts/migrate.sh.
-- ---------------------------------------------------------------------------- --

insert into parents (id, email, full_name, role) values
    ('0192a000-0000-7000-8000-000000000001', 'alice.tan@example.test',      'Alice Tan',      'parent'),
    ('0192a000-0000-7000-8000-000000000002', 'budi.santoso@example.test',   'Budi Santoso',   'parent'),
    ('0192a000-0000-7000-8000-000000000003', 'chandra.wijaya@example.test', 'Chandra Wijaya', 'parent'),
    ('0192a000-0000-7000-8000-000000000009', 'ops.admin@example.test',      'Ops Admin',      'admin');

insert into students (id, parent_id, full_name, grade_level) values
    ('0192a000-0000-7000-8000-000000000011', '0192a000-0000-7000-8000-000000000001', 'Mira Tan',      5),
    ('0192a000-0000-7000-8000-000000000012', '0192a000-0000-7000-8000-000000000001', 'Nico Tan',      3),
    ('0192a000-0000-7000-8000-000000000013', '0192a000-0000-7000-8000-000000000002', 'Dewi Santoso',  6),
    ('0192a000-0000-7000-8000-000000000014', '0192a000-0000-7000-8000-000000000003', 'Eka Wijaya',    4),
    ('0192a000-0000-7000-8000-000000000015', '0192a000-0000-7000-8000-000000000002', 'Fajar Santoso', 5),
    ('0192a000-0000-7000-8000-000000000016', '0192a000-0000-7000-8000-000000000003', 'Gita Wijaya',   5),
    ('0192a000-0000-7000-8000-000000000017', '0192a000-0000-7000-8000-000000000001', 'Hadi Tan',      4);

-- Start times are relative to the seed run, so a seeded class is always in the
-- future no matter when the reviewer clones this.
insert into trial_classes (id, subject, title, starts_at, capacity) values
    ('0192a000-0000-7000-8000-000000000021', 'science', 'Science Discovery',  now() + interval '3 days', 4),
    ('0192a000-0000-7000-8000-000000000022', 'math',    'Math Foundations',   now() + interval '4 days', 4),
    ('0192a000-0000-7000-8000-000000000023', 'science', 'Science Explorers',  now() + interval '5 days', 4),
    ('0192a000-0000-7000-8000-000000000024', 'math',    'Math Challenge',     now() + interval '6 days', 4),
    ('0192a000-0000-7000-8000-000000000025', 'science', 'Science Lab Race',   now() + interval '7 days', 1);

-- What each seeded class proves:
--   ...021 open class:       4 seats, 0 confirmed, the ordinary happy path
--   ...022 nearly full:      4 seats, 3 confirmed, the capacity boundary
--   ...023 duplicate target: 1 confirmed for Mira Tan, the duplicate attempt
--   ...024 failure target:   the payment decline path, the mock provider in
--                            internal/payment declines for this class id
--   ...025 race class:       1 seat, 0 confirmed, the last seat race script

insert into bookings (id, student_id, class_id, status, seat_no, confirmed_at) values
    ('0192a000-0000-7000-8000-000000000031', '0192a000-0000-7000-8000-000000000015', '0192a000-0000-7000-8000-000000000022', 'confirmed', 1, now()),
    ('0192a000-0000-7000-8000-000000000032', '0192a000-0000-7000-8000-000000000016', '0192a000-0000-7000-8000-000000000022', 'confirmed', 2, now()),
    ('0192a000-0000-7000-8000-000000000033', '0192a000-0000-7000-8000-000000000017', '0192a000-0000-7000-8000-000000000022', 'confirmed', 3, now()),
    ('0192a000-0000-7000-8000-000000000034', '0192a000-0000-7000-8000-000000000011', '0192a000-0000-7000-8000-000000000023', 'confirmed', 1, now());

-- A confirmed seat that no money ever paid for would make the reconciliation
-- job lie, so every seeded booking carries its settled attempt.
insert into payment_attempts (id, booking_id, idempotency_key, amount_cents, status, provider_ref, settled_at) values
    ('0192a000-0000-7000-8000-000000000041', '0192a000-0000-7000-8000-000000000031', 'seed-booking-31', 4500, 'succeeded', 'mock_seed_31', now()),
    ('0192a000-0000-7000-8000-000000000042', '0192a000-0000-7000-8000-000000000032', 'seed-booking-32', 4500, 'succeeded', 'mock_seed_32', now()),
    ('0192a000-0000-7000-8000-000000000043', '0192a000-0000-7000-8000-000000000033', 'seed-booking-33', 4500, 'succeeded', 'mock_seed_33', now()),
    ('0192a000-0000-7000-8000-000000000044', '0192a000-0000-7000-8000-000000000034', 'seed-booking-34', 4500, 'succeeded', 'mock_seed_34', now());

insert into booking_events (id, booking_id, from_status, to_status, actor, reason) values
    ('0192a000-0000-7000-8000-000000000051', '0192a000-0000-7000-8000-000000000031', 'pending_payment', 'confirmed', 'payment', 'seeded'),
    ('0192a000-0000-7000-8000-000000000052', '0192a000-0000-7000-8000-000000000032', 'pending_payment', 'confirmed', 'payment', 'seeded'),
    ('0192a000-0000-7000-8000-000000000053', '0192a000-0000-7000-8000-000000000033', 'pending_payment', 'confirmed', 'payment', 'seeded'),
    ('0192a000-0000-7000-8000-000000000054', '0192a000-0000-7000-8000-000000000034', 'pending_payment', 'confirmed', 'payment', 'seeded');
