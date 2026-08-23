import { integer, primaryKey, sqliteTable, text } from 'drizzle-orm/sqlite-core';

export const draftSessions = sqliteTable('draft_sessions', {
  draftId: text('draft_id').primaryKey(),
  userId: text('user_id').notNull(),
  username: text('username').notNull(),
  leagueId: text('league_id'),
  leagueName: text('league_name').notNull(),
  status: text('status').notNull(),
  boardHash: text('board_hash').notNull(),
  stateJson: text('state_json').notNull(),
  createdAt: text('created_at').notNull(),
  updatedAt: text('updated_at').notNull(),
});

export const draftPicks = sqliteTable('draft_picks', {
  draftId: text('draft_id').notNull(),
  pickNo: integer('pick_no').notNull(),
  playerId: text('player_id').notNull(),
  rosterId: text('roster_id').notNull(),
  stateJson: text('state_json').notNull(),
  observedAt: text('observed_at').notNull(),
}, (table) => [primaryKey({ columns: [table.draftId, table.pickNo] })]);

export const recommendations = sqliteTable('recommendations', {
  draftId: text('draft_id').notNull(),
  boardHash: text('board_hash').notNull(),
  playerId: text('player_id').notNull(),
  score: integer('score').notNull(),
  resultJson: text('result_json').notNull(),
  createdAt: text('created_at').notNull(),
}, (table) => [primaryKey({ columns: [table.draftId, table.boardHash] })]);

export const cacheEntries = sqliteTable('cache_entries', {
  key: text('key').primaryKey(),
  valueJson: text('value_json').notNull(),
  expiresAt: text('expires_at').notNull(),
  updatedAt: text('updated_at').notNull(),
});
