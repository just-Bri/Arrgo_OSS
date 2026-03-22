# Plan: Real Database Migrations

## Priority: Medium — Will need to be fixed eventually

## Problem

Schema management is a single SQL blob in `migrations.go` using `IF NOT EXISTS` / `CREATE TABLE` statements. This works for initial setup but has no path for:

- Adding a column to an existing table
- Changing a column type
- Renaming a column or table
- Removing a column
- Adding an index after the fact

Right now, any schema evolution requires either manual `ALTER TABLE` commands or nuking the database. The moment you need to change the schema in a deployed instance, this breaks down.

## Options

### Option A: Numbered SQL files (recommended)

The simplest approach that stays close to raw SQL. No new dependencies.

```
database/
  migrations/
    001_initial_schema.sql
    002_add_subtitle_queue.sql
    003_add_jellyfin_user_id.sql
```

Write a small migration runner in Go:
- Track applied migrations in a `schema_migrations` table
- On startup, run any unapplied migrations in order
- Each migration is a plain `.sql` file — no up/down, just forward

This is ~50 lines of Go code and zero dependencies.

### Option B: Use golang-migrate

[golang-migrate/migrate](https://github.com/golang-migrate/migrate) is the most common Go migration library. Supports up/down migrations, CLI tool, and multiple database drivers.

Heavier than Option A but battle-tested. Adds a dependency.

### Option C: Use goose

[pressly/goose](https://github.com/pressly/goose) is another popular choice. Supports SQL and Go migrations. Slightly nicer CLI than golang-migrate.

## Recommendation

**Option A** for this project. You're already comfortable with raw SQL, the schema isn't huge, and you don't need down-migrations for a personal project. Write a minimal runner, convert the existing schema blob into `001_initial_schema.sql`, and add new migrations as needed.

## Steps

1. Create `database/migrations/` directory.
2. Move the current schema SQL into `001_initial_schema.sql`.
3. Write a `RunMigrations(db *sql.DB)` function that:
   - Creates a `schema_migrations` table if it doesn't exist
   - Reads `.sql` files from the migrations directory
   - Executes any that haven't been recorded yet
   - Records each successful migration
4. Replace `InitSchema()` call in `main.go` with `RunMigrations()`.
5. Going forward, all schema changes go in new numbered files.
