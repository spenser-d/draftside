export interface SleeperUser {
  user_id: string;
  username: string;
  display_name: string;
  avatar: string | null;
}

export interface SleeperNflState {
  season: string;
  previous_season: string;
  league_season: string;
  league_create_season: string;
}

export interface SleeperLeague {
  league_id: string;
  draft_id: string | null;
  name: string;
  season: string;
  status: string;
  total_rosters: number;
  roster_positions: string[];
  scoring_settings: Record<string, number>;
}

export interface SleeperDraft {
  draft_id: string;
  league_id: string | null;
  season: string;
  type: string;
  status: string;
  start_time: number | null;
  created: number;
  settings: Record<string, number | null> & {
    teams: number;
    rounds: number;
    pick_timer: number;
  };
  metadata: Record<string, string | null> | null;
  draft_order: Record<string, number> | null;
  slot_to_roster_id: Record<string, number | string> | null;
}

export interface SleeperPick {
  draft_id: string;
  player_id: string;
  pick_no: number;
  round?: number;
  draft_slot?: number;
  picked_by?: string;
  roster_id?: string | number;
  is_keeper?: boolean | null;
  metadata?: Record<string, unknown>;
}

export interface SleeperTradedPick {
  season: string;
  round: number;
  roster_id: number | string;
  previous_owner_id: number | string;
  owner_id: number | string;
}

export interface SleeperLeagueUser {
  user_id: string;
  display_name: string;
  username: string;
  metadata?: { team_name?: string };
}

export interface SleeperPlayer {
  player_id: string;
  first_name?: string | null;
  last_name?: string | null;
  full_name?: string | null;
  position?: string | null;
  fantasy_positions?: string[] | null;
  team?: string | null;
  active?: boolean;
  status?: string | null;
  injury_status?: string | null;
  search_rank?: number | null;
}
