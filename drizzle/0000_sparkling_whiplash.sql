CREATE TABLE `cache_entries` (
	`key` text PRIMARY KEY NOT NULL,
	`value_json` text NOT NULL,
	`expires_at` text NOT NULL,
	`updated_at` text NOT NULL
);
--> statement-breakpoint
CREATE TABLE `draft_picks` (
	`draft_id` text NOT NULL,
	`pick_no` integer NOT NULL,
	`player_id` text NOT NULL,
	`roster_id` text NOT NULL,
	`state_json` text NOT NULL,
	`observed_at` text NOT NULL,
	PRIMARY KEY(`draft_id`, `pick_no`)
);
--> statement-breakpoint
CREATE TABLE `draft_sessions` (
	`draft_id` text PRIMARY KEY NOT NULL,
	`user_id` text NOT NULL,
	`username` text NOT NULL,
	`league_id` text,
	`league_name` text NOT NULL,
	`status` text NOT NULL,
	`board_hash` text NOT NULL,
	`state_json` text NOT NULL,
	`created_at` text NOT NULL,
	`updated_at` text NOT NULL
);
--> statement-breakpoint
CREATE TABLE `recommendations` (
	`draft_id` text NOT NULL,
	`board_hash` text NOT NULL,
	`player_id` text NOT NULL,
	`score` integer NOT NULL,
	`result_json` text NOT NULL,
	`created_at` text NOT NULL,
	PRIMARY KEY(`draft_id`, `board_hash`)
);
