// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Signed-in profiles.
//!
//! A profile is an account. The webview keeps cookies per *session partition*,
//! so giving each profile its own partition gives it its own `at` cookie, and
//! therefore its own signed-in user, the same idea as a Chrome profile. The work
//! here is not the isolation, which the webview provides, but keeping the list
//! coherent: never empty, never pointing at a profile that is gone, and never
//! producing a partition name that is not safe as a directory.
//!
//! Each profile carries its own server URL and muted-workspace list, because an
//! account exists on one server: "which account" and "which server" are the same
//! question, and splitting them would let you sign in as one user and then point
//! that session at a server where the cookie means nothing.
//!
//! Pure, so the rules are testable without a window. The caller supplies ids and
//! turns partitions into real sessions.
//!
//! Rust notes: every operation takes the state by reference and returns a new
//! one, so "unchanged" is a value comparison (`==`) where the JS has `toBe`. A
//! state that JS tests pass as `null` is an `Option`, and a JS `undefined` or
//! non-string argument folds into the empty string.

use crate::identity::{describe_identity, Account};
use crate::side_panel::child::panel_data_dir;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::HashSet;

/// Shown when a profile has no name of its own.
pub const DEFAULT_PROFILE_LABEL: &str = "Default";

/// The id a first run or a pre-profiles config gets.
pub const FIRST_PROFILE_ID: &str = "default";

/// Longest label kept; anything more is a paste accident, not a name.
pub const MAX_LABEL: usize = 40;

/// One profile, with everything absent or malformed filled in.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Profile {
    pub id: String,
    pub label: String,
    pub partition: String,
    pub server_url: String,
    pub muted_workspaces: Vec<String>,
    /// Who this profile belongs to, as last seen. Stored, not looked up, so a
    /// server that is slow, unreachable, or has expired the session cannot make
    /// a profile anonymous; see [`remember_account`].
    #[serde(skip_serializing_if = "Option::is_none")]
    pub account: Option<Account>,
}

/// The list of profiles and the one in use.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ProfilesState {
    pub profiles: Vec<Profile>,
    pub active_profile_id: String,
}

/// What [`update_profile`] may change; a field left `None` is left alone.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct ProfilePatch {
    pub server_url: Option<String>,
    pub muted_workspaces: Option<Vec<String>>,
    pub label: Option<String>,
}

/// Partition name for a new profile.
///
/// `persist:` is what makes the jar survive a restart; without it, signing in
/// would last only as long as the window. The id is embedded directly, which is
/// why ids are restricted to characters that are safe in a directory name: the
/// partition becomes a folder under the app's Partitions directory.
pub fn partition_for(id: &str) -> String {
    format!("persist:profile-{id}")
}

/// Ids are used in a filesystem path, so they are deliberately narrow:
/// `^[a-z0-9][a-z0-9-]{0,63}$`.
pub fn is_valid_profile_id(id: &str) -> bool {
    let mut bytes = id.bytes();
    id.len() <= 64
        && bytes
            .next()
            .is_some_and(|b| b.is_ascii_lowercase() || b.is_ascii_digit())
        && bytes.all(|b| b.is_ascii_lowercase() || b.is_ascii_digit() || b == b'-')
}

/// Whitespace collapsed and trimmed; `fallback` when nothing is left.
pub(crate) fn clean_label(label: Option<&str>, fallback: &str) -> String {
    let collapsed = label
        .unwrap_or("")
        .split_whitespace()
        .collect::<Vec<_>>()
        .join(" ");
    if collapsed.is_empty() {
        fallback.to_string()
    } else {
        collapsed.chars().take(MAX_LABEL).collect()
    }
}

pub(crate) fn clean_muted<S: AsRef<str>>(ids: impl IntoIterator<Item = S>) -> Vec<String> {
    let mut seen = HashSet::new();
    ids.into_iter()
        .map(|id| id.as_ref().to_string())
        .filter(|id| !id.is_empty() && seen.insert(id.clone()))
        .collect()
}

fn muted_from_json(value: Option<&Value>) -> Vec<String> {
    match value {
        Some(Value::Array(items)) => clean_muted(items.iter().filter_map(Value::as_str)),
        _ => Vec::new(),
    }
}

pub(crate) fn str_field<'a>(raw: &'a Value, key: &str) -> Option<&'a str> {
    raw.get(key).and_then(Value::as_str)
}

/// One stored profile. `None` when the id is unusable, since a profile without a
/// valid id has no partition and cannot be stored.
pub fn make_profile(raw: &Value) -> Option<Profile> {
    let id = str_field(raw, "id").filter(|id| is_valid_profile_id(id))?;
    Some(Profile {
        id: id.to_string(),
        label: clean_label(str_field(raw, "label"), DEFAULT_PROFILE_LABEL),
        // Stored rather than derived so the name is stable if the scheme ever
        // changes: a partition that moved would be an empty jar, a sign-out.
        partition: str_field(raw, "partition")
            .filter(|partition| !partition.is_empty())
            .map_or_else(|| partition_for(id), str::to_string),
        server_url: str_field(raw, "serverUrl").unwrap_or("").to_string(),
        muted_workspaces: muted_from_json(raw.get("mutedWorkspaces")),
        account: raw.get("account").and_then(describe_identity),
    })
}

/// A name for a new profile that is not the name every other one already has.
///
/// "Default" for all of them is how a switcher becomes unreadable the moment it
/// cannot reach the server: every row falls back to its label and they are all
/// the same word. The first profile keeps the name it has always had; the ones
/// after it are numbered, and the number skips anything already taken so
/// renaming one to "Profile 3" does not produce a second.
pub fn next_profile_label(state: Option<&ProfilesState>) -> String {
    let profiles = state.map_or(&[][..], |state| &state.profiles[..]);
    if profiles.is_empty() {
        return DEFAULT_PROFILE_LABEL.to_string();
    }

    let taken: HashSet<&str> = profiles.iter().map(|p| p.label.as_str()).collect();
    let mut n = profiles.len() + 1;
    while taken.contains(format!("Profile {n}").as_str()) {
        n += 1;
    }
    format!("Profile {n}")
}

/// Record who a profile is signed in as.
///
/// The rule that matters is what it does with nothing: a lookup that failed
/// leaves the profile as it was. Overwriting the account with "could not say" is
/// how every profile in the switcher turned back into its label: the session is
/// good for a day, the lookup gives up after four seconds, and the app has no
/// memory across a restart, so all three ordinary conditions ended with a list
/// of profiles called "Default".
///
/// What is remembered is who a profile *belongs to*, which does not stop being
/// true when the session expires. Whether it is signed in right now is a
/// separate question, asked live and answered in the subtitle.
pub fn remember_account(state: &ProfilesState, id: &str, identity: &Value) -> ProfilesState {
    let Some(account) = describe_identity(identity) else {
        return state.clone();
    };
    let mut next = state.clone();
    if let Some(profile) = next.profiles.iter_mut().find(|p| p.id == id) {
        profile.account = Some(account);
    }
    next
}

/// The profile that already holds this one's account, if any.
///
/// Two profiles signed into the same account are two windows onto one thing: the
/// same tasks, the same workspaces, the same notifications twice. Nothing can
/// stop it at the moment a profile is added (it has no account yet, and the
/// sign-in happens on a web page the shell does not drive), so it is caught when
/// the account becomes known, and said out loud where the profiles are listed.
///
/// The *earlier* profile wins, always: whichever was added first is the
/// original, and the answer does not change depending on which one you ask
/// about.
///
/// Returns the other profile's id, or `""` when this one is not a duplicate.
pub fn duplicate_of(state: Option<&ProfilesState>, id: &str) -> String {
    let profiles = state.map_or(&[][..], |state| &state.profiles[..]);
    let Some(position) = profiles.iter().position(|p| p.id == id) else {
        return String::new();
    };
    let Some(key) = profiles[position].account.as_ref().map(Account::key) else {
        return String::new();
    };
    if key.is_empty() {
        return String::new();
    }

    // Only what was added before this one, which is what makes the answer
    // stable: asked about either half of a pair, it names the same original.
    profiles[..position]
        .iter()
        .find(|p| p.account.as_ref().map(Account::key).as_deref() == Some(key.as_str()))
        .map(|p| p.id.clone())
        .unwrap_or_default()
}

/// Bring stored profile state forward, from any shape including none.
///
/// A config from before profiles existed becomes a single profile carrying the
/// settings that were already there, so upgrading does not look like being
/// signed out; the cookie jar is separate from this.
///
/// `first_id` is the id for the migrated profile when there is none; an unusable
/// one becomes [`FIRST_PROFILE_ID`].
pub fn migrate_profiles(raw: &Value, first_id: &str) -> ProfilesState {
    let mut profiles: Vec<Profile> = match raw.get("profiles") {
        Some(Value::Array(items)) => items.iter().filter_map(make_profile).collect(),
        _ => Vec::new(),
    };

    if profiles.is_empty() {
        // Either a first run or a pre-profiles config. Both become one profile;
        // the second keeps the server and mutes that were already configured.
        let id = if is_valid_profile_id(first_id) {
            first_id
        } else {
            FIRST_PROFILE_ID
        };
        profiles.push(Profile {
            id: id.to_string(),
            label: DEFAULT_PROFILE_LABEL.to_string(),
            partition: partition_for(id),
            server_url: str_field(raw, "serverUrl").unwrap_or("").to_string(),
            muted_workspaces: muted_from_json(raw.get("mutedWorkspaces")),
            account: None,
        });
    }

    // Two profiles sharing an id would share a partition, which is the one thing
    // this must never allow: the same account wearing two names.
    let mut seen = HashSet::new();
    profiles.retain(|profile| seen.insert(profile.id.clone()));

    let active_profile_id = str_field(raw, "activeProfileId")
        .filter(|id| profiles.iter().any(|profile| profile.id == *id))
        .unwrap_or(&profiles[0].id)
        .to_string();
    ProfilesState {
        profiles,
        active_profile_id,
    }
}

/// The profile in use. Never missing: `migrate_profiles` guarantees one exists.
pub fn active_profile(state: &ProfilesState) -> &Profile {
    state
        .profiles
        .iter()
        .find(|p| p.id == state.active_profile_id)
        .unwrap_or(&state.profiles[0])
}

/// Add a profile and switch to it; the state itself when the id is unusable or
/// already taken.
pub fn add_profile(
    state: &ProfilesState,
    id: &str,
    label: &str,
    server_url: &str,
) -> ProfilesState {
    // Always its own jar: a second profile sharing the first's session would be
    // the same signed-in account under a different name.
    //
    // An unnamed profile is numbered rather than called "Default" like the first
    // one: until it has signed in, its label is the only thing distinguishing it
    // in the switcher, and two identical labels distinguish nothing.
    let named = if label.trim().is_empty() {
        next_profile_label(Some(state))
    } else {
        label.to_string()
    };
    let Some(profile) = make_profile(&serde_json::json!({
        "id": id,
        "label": named,
        "serverUrl": server_url,
        "partition": partition_for(id),
    })) else {
        return state.clone();
    };
    if state.profiles.iter().any(|p| p.id == profile.id) {
        return state.clone();
    }

    // A new profile is switched to: adding one you then have to go and select is
    // a step nobody wants.
    let active_profile_id = profile.id.clone();
    let mut profiles = state.profiles.clone();
    profiles.push(profile);
    ProfilesState {
        profiles,
        active_profile_id,
    }
}

/// Remove a profile.
///
/// The last one cannot be removed: an app with no profile has no session to run
/// in. Removing the active one falls back to `fallback_id` when that profile is
/// still there, and to the first remaining otherwise.
///
/// The fallback exists for abandoning a profile that was just added: the one to
/// land on is the one it was added *from*, which with three or more profiles is
/// not the first in the list.
pub fn remove_profile(state: &ProfilesState, id: &str, fallback_id: &str) -> ProfilesState {
    if state.profiles.len() <= 1 {
        return state.clone();
    }
    let profiles: Vec<Profile> = state
        .profiles
        .iter()
        .filter(|p| p.id != id)
        .cloned()
        .collect();
    if profiles.len() == state.profiles.len() {
        return state.clone();
    }

    let active_profile_id = if profiles.iter().any(|p| p.id == state.active_profile_id) {
        state.active_profile_id.clone()
    } else if profiles.iter().any(|p| p.id == fallback_id) {
        fallback_id.to_string()
    } else {
        profiles[0].id.clone()
    };
    ProfilesState {
        profiles,
        active_profile_id,
    }
}

/// Whether the profile on screen can be thrown away instead of finished.
///
/// A profile with no server has never been used for anything: it was added and
/// then abandoned at the connection screen. Discarding it is only offered when
/// another profile remains to go back to; during a genuine first run there is
/// nowhere to go, and the app needs a server before it can do anything at all.
///
/// One rule in one place because two callers ask it: the connection screen, to
/// decide whether to offer a way out, and the shell, before taking one.
pub fn can_discard_active_profile(state: Option<&ProfilesState>) -> bool {
    match state {
        Some(state) if state.profiles.len() > 1 => active_profile(state).server_url.is_empty(),
        _ => false,
    }
}

/// Rename a profile. A blank name keeps the old one.
pub fn rename_profile(state: &ProfilesState, id: &str, label: &str) -> ProfilesState {
    update_profile(
        state,
        id,
        &ProfilePatch {
            label: Some(label.to_string()),
            ..ProfilePatch::default()
        },
    )
}

/// Switch profiles. An unknown id changes nothing.
pub fn activate_profile(state: &ProfilesState, id: &str) -> ProfilesState {
    if !state.profiles.iter().any(|p| p.id == id) {
        return state.clone();
    }
    ProfilesState {
        active_profile_id: id.to_string(),
        ..state.clone()
    }
}

/// One profile's fields updated; an unknown id changes nothing.
pub fn update_profile(state: &ProfilesState, id: &str, patch: &ProfilePatch) -> ProfilesState {
    let mut next = state.clone();
    if let Some(profile) = next.profiles.iter_mut().find(|p| p.id == id) {
        if let Some(server_url) = &patch.server_url {
            profile.server_url = server_url.clone();
        }
        if let Some(muted) = &patch.muted_workspaces {
            profile.muted_workspaces = clean_muted(muted);
        }
        if let Some(label) = &patch.label {
            profile.label = clean_label(Some(label), &profile.label);
        }
    }
    next
}

/// The WKWebView data store a profile's webviews use on macOS 14 and later,
/// derived from its id so a profile finds its own store again after a restart.
///
/// The store is named by a UUID: this is a 128-bit FNV-1a of the id, stamped as
/// a version 8 (custom) UUID. Not a secret and not a boundary against the app
/// itself, only a name that two profiles never share in practice.
pub fn data_store_id(id: &str) -> [u8; 16] {
    const OFFSET: u128 = 0x6c62_272e_07bb_0142_62b8_2175_6295_c58d;
    const PRIME: u128 = 0x0000_0000_0100_0000_0000_0000_0000_013b;
    let hash = id
        .bytes()
        .fold(OFFSET, |hash, b| (hash ^ u128::from(b)).wrapping_mul(PRIME));
    let mut uuid = hash.to_be_bytes();
    uuid[6] = (uuid[6] & 0x0f) | 0x80;
    uuid[8] = (uuid[8] & 0x3f) | 0x80;
    uuid
}

/// The folders under the profiles directory that belong to no profile.
///
/// A removed profile's folder is deleted at once where the OS allows it, but a
/// webview that was just closed can still hold its files open for a moment
/// (WebView2 does), so whatever is left is swept on the next start. Only a name
/// a profile could have had is returned: a folder put there by anything else is
/// not this app's to delete.
pub fn orphaned_profile_dirs<'a>(names: &'a [String], state: &ProfilesState) -> Vec<&'a str> {
    names
        .iter()
        .map(String::as_str)
        .filter(|name| is_valid_profile_id(name) && !state.profiles.iter().any(|p| p.id == *name))
        .collect()
}

/// The macOS data stores that belong to no profile: a removed profile's store
/// is deleted once its window is gone, and whatever a quit cut short is swept
/// on the next start. Every store the app has is one of its profiles' own, or
/// the one its side panel's guests share ([`profile_stores`]).
pub fn orphaned_data_stores(stores: &[[u8; 16]], state: &ProfilesState) -> Vec<[u8; 16]> {
    stores
        .iter()
        .filter(|store| {
            !state
                .profiles
                .iter()
                .any(|p| profile_stores(&p.id).contains(store))
        })
        .copied()
        .collect()
}

/// A profile's macOS data stores: its own, and its side panel guests'.
pub fn profile_stores(id: &str) -> [[u8; 16]; 2] {
    [data_store_id(id), data_store_id(&panel_data_dir(id))]
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn base() -> ProfilesState {
        migrate_profiles(&Value::Null, FIRST_PROFILE_ID)
    }

    fn add(state: &ProfilesState, id: &str, label: &str) -> ProfilesState {
        add_profile(state, id, label, "")
    }

    fn with_server(state: &ProfilesState, id: &str, url: &str) -> ProfilesState {
        update_profile(
            state,
            id,
            &ProfilePatch {
                server_url: Some(url.to_string()),
                ..ProfilePatch::default()
            },
        )
    }

    fn ids(state: &ProfilesState) -> Vec<&str> {
        state.profiles.iter().map(|p| p.id.as_str()).collect()
    }

    fn labels(state: &ProfilesState) -> Vec<&str> {
        state.profiles.iter().map(|p| p.label.as_str()).collect()
    }

    // migrateProfiles

    #[test]
    fn gives_a_first_run_one_profile_to_start_from() {
        let state = migrate_profiles(&Value::Null, FIRST_PROFILE_ID);

        assert_eq!(state.profiles.len(), 1);
        assert_eq!(state.profiles[0].label, DEFAULT_PROFILE_LABEL);
        assert_eq!(state.active_profile_id, state.profiles[0].id);
    }

    #[test]
    fn carries_a_pre_profiles_config_into_the_first_profile() {
        // Upgrading must not look like being reconfigured: the server and the
        // mute list were already chosen and belong to the account already
        // signed in.
        let state = migrate_profiles(
            &json!({
                "version": 2,
                "serverUrl": "https://app.agentrq.com",
                "mutedWorkspaces": ["ws1", "ws2"],
            }),
            FIRST_PROFILE_ID,
        );

        assert_eq!(state.profiles[0].server_url, "https://app.agentrq.com");
        assert_eq!(state.profiles[0].muted_workspaces, ["ws1", "ws2"]);
    }

    #[test]
    fn gives_the_migrated_profile_a_partition_like_any_other() {
        // Its `at` cookie is in the default session, so this signs the user out
        // once on upgrade. Agreed trade: the session lasts 24 hours anyway, and
        // the alternative is one profile that is special-cased wherever a
        // session is resolved.
        let state = migrate_profiles(
            &json!({"version": 2, "serverUrl": "https://x.test"}),
            FIRST_PROFILE_ID,
        );

        assert_eq!(
            state.profiles[0].partition,
            partition_for(&state.profiles[0].id)
        );
        assert_ne!(state.profiles[0].partition, "");
    }

    #[test]
    fn repairs_a_stored_profile_that_has_no_partition_of_its_own() {
        // An empty partition would mean the shared default session, which is
        // the one thing a profile must never have.
        let state = migrate_profiles(
            &json!({"profiles": [{"id": "one", "label": "X", "partition": ""}]}),
            FIRST_PROFILE_ID,
        );

        assert_eq!(state.profiles[0].partition, partition_for("one"));
    }

    #[test]
    fn keeps_profiles_that_were_already_stored() {
        let state = migrate_profiles(
            &json!({
                "profiles": [
                    {"id": "one", "label": "Personal", "partition": "persist:profile-one",
                     "serverUrl": "https://a.test"},
                    {"id": "two", "label": "Work", "partition": "persist:profile-two",
                     "serverUrl": "https://b.test"},
                ],
                "activeProfileId": "two",
            }),
            FIRST_PROFILE_ID,
        );

        assert_eq!(labels(&state), ["Personal", "Work"]);
        assert_eq!(state.active_profile_id, "two");
    }

    #[test]
    fn refuses_two_profiles_sharing_an_id() {
        // They would share a partition, so they would be one account wearing
        // two names, the single thing this list must never contain.
        let state = migrate_profiles(
            &json!({"profiles": [
                {"id": "one", "label": "First"},
                {"id": "one", "label": "Second"},
            ]}),
            FIRST_PROFILE_ID,
        );

        assert_eq!(state.profiles.len(), 1);
        assert_eq!(state.profiles[0].label, "First");
    }

    #[test]
    fn drops_entries_whose_id_could_not_be_a_directory_name() {
        // The id becomes a folder under the app's Partitions directory.
        let state = migrate_profiles(
            &json!({"profiles": [
                {"id": "../escape", "label": "Bad"},
                {"id": "Uppercase", "label": "Also bad"},
                {"id": "fine", "label": "Good"},
            ]}),
            FIRST_PROFILE_ID,
        );

        assert_eq!(ids(&state), ["fine"]);
    }

    #[test]
    fn falls_back_to_the_first_profile_when_the_active_id_names_nothing() {
        let state = migrate_profiles(
            &json!({"profiles": [{"id": "one", "label": "Personal"}], "activeProfileId": "deleted"}),
            FIRST_PROFILE_ID,
        );

        assert_eq!(state.active_profile_id, "one");
    }

    #[test]
    fn never_returns_an_empty_list_whatever_it_is_handed() {
        // `undefined` folds into `null`.
        for raw in [
            json!(null),
            json!([]),
            json!("nonsense"),
            json!(42),
            json!({"profiles": "no"}),
            json!({"profiles": []}),
        ] {
            assert!(
                !migrate_profiles(&raw, FIRST_PROFILE_ID).profiles.is_empty(),
                "{raw}"
            );
        }
    }

    // addProfile

    #[test]
    fn gives_a_new_profile_its_own_session() {
        let state = add(&base(), "work1", "Work");

        assert_eq!(state.profiles.len(), 2);
        assert_eq!(state.profiles[1].partition, partition_for("work1"));
        assert_ne!(state.profiles[1].partition, state.profiles[0].partition);
    }

    #[test]
    fn switches_to_what_it_just_added() {
        // Adding a profile you then have to go and select is a step nobody
        // wants.
        assert_eq!(add(&base(), "work1", "Work").active_profile_id, "work1");
    }

    #[test]
    fn refuses_an_id_already_in_use() {
        let state = add(&base(), "work1", "Work");

        assert_eq!(add(&state, "work1", "Other"), state);
    }

    #[test]
    fn refuses_an_unusable_id() {
        let state = base();
        // `undefined` folds into the empty string.
        for id in ["", "../x", "has space", "CAPS"] {
            assert_eq!(add(&state, id, "x"), state, "{id}");
        }
    }

    // Not "Default" like the first one: until a profile has signed in, its
    // label is the only thing distinguishing it in the switcher, and a list of
    // identical labels distinguishes nothing.
    #[test]
    fn names_an_unnamed_profile_rather_than_leaving_it_blank() {
        let state = add(&base(), "x1", "   ");

        assert_eq!(state.profiles[1].label, "Profile 2");
        assert_ne!(state.profiles[1].label, state.profiles[0].label);
    }

    #[test]
    fn keeps_a_name_that_was_given() {
        assert_eq!(add(&base(), "x1", "Work").profiles[1].label, "Work");
    }

    #[test]
    fn starts_a_new_profile_on_the_server_it_is_given() {
        // Not in the vitest file: the `serverUrl` option of `addProfile`.
        let state = add_profile(&base(), "x1", "Work", "https://work.test");

        assert_eq!(state.profiles[1].server_url, "https://work.test");
    }

    // nextProfileLabel

    #[test]
    fn is_the_first_name_when_there_are_no_profiles_at_all() {
        let empty = ProfilesState {
            profiles: vec![],
            active_profile_id: String::new(),
        };

        assert_eq!(next_profile_label(Some(&empty)), DEFAULT_PROFILE_LABEL);
        assert_eq!(next_profile_label(None), DEFAULT_PROFILE_LABEL);
    }

    #[test]
    fn numbers_from_the_count_so_the_second_profile_is_profile_2() {
        assert_eq!(next_profile_label(Some(&base())), "Profile 2");
    }

    // Otherwise renaming a profile to "Profile 3" by hand would produce a
    // second one with the same name, which is the problem this exists to avoid.
    #[test]
    fn skips_a_number_somebody_has_already_used() {
        let state = migrate_profiles(
            &json!({"profiles": [
                {"id": "a", "label": DEFAULT_PROFILE_LABEL},
                {"id": "b", "label": "Profile 3"},
            ], "activeProfileId": "a"}),
            FIRST_PROFILE_ID,
        );

        assert_eq!(next_profile_label(Some(&state)), "Profile 4");
    }

    // removeProfile

    #[test]
    fn refuses_to_remove_the_last_one() {
        // An app with no profile has no session to run in.
        let state = base();

        assert_eq!(remove_profile(&state, &state.profiles[0].id, ""), state);
    }

    #[test]
    fn moves_off_a_profile_it_just_removed() {
        let state = add(&base(), "work1", "Work");
        assert_eq!(state.active_profile_id, "work1");

        let after = remove_profile(&state, "work1", "");

        assert_eq!(after.profiles.len(), 1);
        assert_eq!(after.active_profile_id, after.profiles[0].id);
    }

    #[test]
    fn leaves_the_active_profile_alone_when_another_is_removed() {
        let state = add(&add(&base(), "work1", "Work"), "work2", "Second");

        assert_eq!(
            remove_profile(&state, "work1", "").active_profile_id,
            "work2"
        );
    }

    #[test]
    fn ignores_an_id_it_does_not_have() {
        let state = add(&base(), "work1", "Work");

        assert_eq!(remove_profile(&state, "nope", ""), state);
    }

    #[test]
    fn goes_back_to_the_profile_the_caller_names() {
        // Abandoning a profile has to land on the one it was added from. With
        // three profiles that is not the first in the list, so falling back to
        // profiles[0] would drop the user into a third account they never
        // chose.
        let mut state = add(&base(), "work1", "Work");
        state = add(&state, "work2", "Second");
        state = add(&state, "work3", "Third");

        assert_eq!(
            remove_profile(&state, "work3", "work1").active_profile_id,
            "work1"
        );
    }

    #[test]
    fn falls_back_to_the_first_remaining_when_the_named_one_is_gone_too() {
        let state = add(&base(), "work1", "Work");

        let after = remove_profile(&state, "work1", "vanished");

        assert_eq!(after.active_profile_id, after.profiles[0].id);
    }

    #[test]
    fn never_lands_on_the_profile_it_just_removed() {
        let state = add(&base(), "work1", "Work");

        assert_ne!(
            remove_profile(&state, "work1", "work1").active_profile_id,
            "work1"
        );
    }

    #[test]
    fn leaves_an_untouched_active_profile_alone_even_when_a_fallback_is_named() {
        let state = add(&add(&base(), "work1", "Work"), "work2", "Second");

        assert_eq!(
            remove_profile(&state, "work1", "nonsense").active_profile_id,
            "work2"
        );
    }

    // canDiscardActiveProfile

    #[test]
    fn says_no_during_a_first_run() {
        // The only profile there is, and no server yet: this is the screen the
        // app cannot start without, not a step the user chose to take.
        assert!(!can_discard_active_profile(Some(&base())));
    }

    #[test]
    fn says_no_when_the_only_profile_is_configured() {
        let state = with_server(
            &base(),
            &base().active_profile_id,
            "https://app.agentrq.com",
        );

        assert!(!can_discard_active_profile(Some(&state)));
    }

    #[test]
    fn says_yes_for_a_profile_added_and_never_connected() {
        let mut state = with_server(
            &base(),
            &base().active_profile_id,
            "https://app.agentrq.com",
        );
        state = add(&state, "work1", "Work");

        assert!(can_discard_active_profile(Some(&state)));
    }

    #[test]
    fn says_no_once_that_profile_has_a_server() {
        // It is a working account now; the way out of it is the switcher.
        let mut state = add(&base(), "work1", "Work");
        state = with_server(&state, "work1", "https://work.example.com");

        assert!(!can_discard_active_profile(Some(&state)));
    }

    #[test]
    fn says_no_rather_than_throwing_when_there_is_no_state_at_all() {
        // Called from the shell, which can ask before the profiles are loaded.
        // `{}` is a state with no profiles.
        let empty = ProfilesState {
            profiles: vec![],
            active_profile_id: String::new(),
        };

        assert!(!can_discard_active_profile(None));
        assert!(!can_discard_active_profile(Some(&empty)));
    }

    // activateProfile

    #[test]
    fn switches_to_a_profile_that_exists() {
        let state = add(&base(), "work1", "Work");
        let first = state.profiles[0].id.clone();

        assert_eq!(activate_profile(&state, &first).active_profile_id, first);
    }

    #[test]
    fn ignores_one_that_does_not_rather_than_leaving_nothing_active() {
        let state = base();

        assert_eq!(activate_profile(&state, "ghost"), state);
    }

    // renameProfile and updateProfile

    #[test]
    fn renames_tidying_the_label() {
        let state = rename_profile(&base(), &base().profiles[0].id, "  Personal   account ");

        assert_eq!(state.profiles[0].label, "Personal account");
    }

    #[test]
    fn keeps_the_old_name_rather_than_accepting_a_blank_one() {
        let id = base().profiles[0].id.clone();

        assert_eq!(
            rename_profile(&base(), &id, "   ").profiles[0].label,
            DEFAULT_PROFILE_LABEL
        );
    }

    #[test]
    fn trims_a_label_that_is_really_a_paste_accident() {
        let id = base().profiles[0].id.clone();

        assert_eq!(
            rename_profile(&base(), &id, &"x".repeat(200)).profiles[0]
                .label
                .len(),
            40
        );
    }

    #[test]
    fn updates_the_server_and_mutes_of_one_profile_only() {
        let mut state = add(&base(), "work1", "Work");
        let other = state.profiles[0].id.clone();

        state = update_profile(
            &state,
            "work1",
            &ProfilePatch {
                server_url: Some("https://work.test".into()),
                muted_workspaces: Some(["a", "a", "b", ""].into_iter().map(String::from).collect()),
                label: None,
            },
        );

        assert_eq!(state.profiles[1].server_url, "https://work.test");
        assert_eq!(state.profiles[1].muted_workspaces, ["a", "b"]);
        // The other profile is untouched: settings belong to one account, not
        // all.
        let untouched = state.profiles.iter().find(|p| p.id == other).unwrap();
        assert_eq!(untouched.server_url, "");
    }

    #[test]
    fn ignores_an_unknown_profile() {
        let state = base();

        assert_eq!(rename_profile(&state, "ghost", "x"), state);
        assert_eq!(with_server(&state, "ghost", "https://x.test"), state);
    }

    // activeProfile

    #[test]
    fn returns_the_one_in_use() {
        let state = add(&base(), "work1", "Work");

        assert_eq!(active_profile(&state).id, "work1");
    }

    #[test]
    fn falls_back_to_the_first_rather_than_returning_nothing() {
        let state = ProfilesState {
            active_profile_id: "ghost".into(),
            ..base()
        };

        assert_eq!(active_profile(&state), &state.profiles[0]);
    }

    // ids and partitions

    #[test]
    fn accepts_ids_that_are_safe_in_a_path_and_rejects_the_rest() {
        for id in ["default", "a1b2c3", "work-2"] {
            assert!(is_valid_profile_id(id), "{id}");
        }
        // `null` and `7` are not strings, so they have no Rust counterpart.
        for id in ["", "../x", "a/b", "CAPS", "-lead", &"x".repeat(65)] {
            assert!(!is_valid_profile_id(id), "{id}");
        }
    }

    #[test]
    fn builds_a_persistent_partition_so_a_sign_in_outlives_the_window() {
        assert_eq!(partition_for("a1b2c3"), "persist:profile-a1b2c3");
    }

    #[test]
    fn rejects_a_profile_with_no_usable_id() {
        assert_eq!(make_profile(&json!({"id": "../escape"})), None);
        assert_eq!(make_profile(&json!(null)), None);
    }

    // edges the caller can reach

    #[test]
    fn names_a_profile_whose_label_is_not_text_at_all() {
        // Stored state is read off disk, so a label can be any JSON value.
        for label in [json!(42), json!(null), json!({}), json!(["x"]), json!(true)] {
            assert_eq!(
                make_profile(&json!({"id": "one", "label": label}))
                    .unwrap()
                    .label,
                DEFAULT_PROFILE_LABEL,
                "{label}"
            );
        }
    }

    #[test]
    fn still_produces_a_usable_profile_when_handed_an_unusable_first_id() {
        // The caller supplies this id; it must not be able to produce a profile
        // with no partition, which would mean the shared default session.
        // `undefined` and `7` are not strings, so they fold into "".
        for bad in ["../escape", "CAPS", ""] {
            let state = migrate_profiles(&json!({"serverUrl": "https://x.test"}), bad);

            assert_eq!(state.profiles.len(), 1);
            assert!(is_valid_profile_id(&state.profiles[0].id));
            assert_eq!(
                state.profiles[0].partition,
                partition_for(&state.profiles[0].id)
            );
        }
    }

    #[test]
    fn uses_a_usable_first_id_as_it_is() {
        // Not in the vitest file: the other branch of the same check.
        assert_eq!(migrate_profiles(&Value::Null, "p1").profiles[0].id, "p1");
    }

    #[test]
    fn renames_through_update_profile_as_well_as_rename_profile() {
        let state = update_profile(
            &base(),
            &base().profiles[0].id,
            &ProfilePatch {
                label: Some("  Personal  ".into()),
                ..ProfilePatch::default()
            },
        );

        assert_eq!(state.profiles[0].label, "Personal");
    }

    #[test]
    fn leaves_the_name_alone_when_update_profile_is_given_none() {
        let state = with_server(&base(), &base().profiles[0].id, "https://x.test");

        assert_eq!(state.profiles[0].label, DEFAULT_PROFILE_LABEL);
    }

    // rememberAccount

    fn sam() -> Account {
        Account {
            name: "Sam".into(),
            email: "sam@example.com".into(),
            picture: String::new(),
        }
    }

    fn sam_json() -> Value {
        json!({"name": "Sam", "email": "sam@example.com", "picture": ""})
    }

    #[test]
    fn records_who_a_profile_is_signed_in_as() {
        let state = remember_account(&base(), &base().profiles[0].id, &sam_json());

        assert_eq!(state.profiles[0].account, Some(sam()));
    }

    // The rule this whole thing exists for. The session is good for a day, the
    // lookup gives up after four seconds, and every start begins knowing
    // nothing, so "could not say" arrives constantly, and writing it down turned
    // the switcher back into a list of profiles called "Default".
    #[test]
    fn keeps_what_it_knows_when_a_lookup_could_not_answer() {
        let known = remember_account(&base(), &base().profiles[0].id, &sam_json());

        // `undefined` folds into `null`.
        for nothing in [
            json!(null),
            json!({}),
            json!({"name": "", "email": ""}),
            json!("sam@example.com"),
        ] {
            assert_eq!(
                remember_account(&known, &known.profiles[0].id, &nothing),
                known,
                "{nothing}"
            );
        }
        assert_eq!(known.profiles[0].account, Some(sam()));
    }

    #[test]
    fn leaves_a_profile_it_does_not_have_alone() {
        let state = base();

        assert_eq!(remember_account(&state, "nobody", &sam_json()), state);
    }

    #[test]
    fn survives_being_stored_and_read_back() {
        let state = remember_account(&base(), &base().profiles[0].id, &sam_json());

        let stored = serde_json::to_value(&state).unwrap();
        let reloaded = migrate_profiles(&stored, FIRST_PROFILE_ID);

        assert_eq!(reloaded.profiles[0].account, Some(sam()));
    }

    // duplicateOf

    fn two_profiles() -> (ProfilesState, String) {
        let state = base();
        let first = state.profiles[0].id.clone();
        let state = add(&state, "second", "Work");
        (remember_account(&state, &first, &sam_json()), first)
    }

    // Two profiles signed into one account are two windows onto the same thing:
    // the same tasks, the same workspaces, every notification twice.
    #[test]
    fn names_the_profile_that_already_holds_this_account() {
        let (state, first) = two_profiles();
        let with_dupe = remember_account(
            &state,
            "second",
            &json!({"name": "Sam", "email": "sam@example.com"}),
        );

        assert_eq!(duplicate_of(Some(&with_dupe), "second"), first);
    }

    // A person types their address however they please, and the account is the
    // same one either way.
    #[test]
    fn does_not_care_how_the_address_was_typed() {
        let (state, first) = two_profiles();
        let with_dupe = remember_account(&state, "second", &json!({"email": "  Sam@Example.COM "}));

        assert_eq!(duplicate_of(Some(&with_dupe), "second"), first);
    }

    // Whichever was added first is the original, and the answer must not depend
    // on which of the two you ask about.
    #[test]
    fn points_at_the_earlier_profile_not_the_later_one() {
        let (state, first) = two_profiles();
        let with_dupe = remember_account(&state, "second", &json!({"email": "sam@example.com"}));

        assert_eq!(duplicate_of(Some(&with_dupe), &first), "");
    }

    #[test]
    fn is_nothing_for_a_profile_on_its_own_account() {
        let (state, _) = two_profiles();
        let other = remember_account(&state, "second", &json!({"email": "alex@example.com"}));

        assert_eq!(duplicate_of(Some(&other), "second"), "");
    }

    // Two profiles nobody can identify are not a duplicate: we do not know that
    // they are the same, only that we cannot tell.
    #[test]
    fn is_nothing_when_there_is_no_account_to_compare() {
        let (state, _) = two_profiles();

        assert_eq!(duplicate_of(Some(&state), "second"), "");
        assert_eq!(duplicate_of(Some(&state), "nobody"), "");
        assert_eq!(duplicate_of(None, "second"), "");
    }

    #[test]
    fn is_nothing_for_an_account_that_has_a_name_but_no_address() {
        // Not in the vitest file: `accountKey` is empty, which equals nothing.
        let (state, first) = two_profiles();
        let nameless = remember_account(&state, &first, &json!({"name": "Sam"}));
        let both = remember_account(&nameless, "second", &json!({"name": "Sam"}));

        assert_eq!(duplicate_of(Some(&both), "second"), "");
    }

    // data_store_id, orphaned_profile_dirs (Tauri only: Electron has partitions)

    #[test]
    fn gives_each_profile_a_stable_store_of_its_own() {
        let first = data_store_id(FIRST_PROFILE_ID);
        assert_eq!(
            first,
            data_store_id("default"),
            "the same store after a restart"
        );
        assert_ne!(first, data_store_id("defaulu"));
        assert_ne!(
            data_store_id("0123456789abcdef"),
            data_store_id("0123456789abcdee")
        );
        assert_ne!(data_store_id(""), data_store_id("a"));
        for uuid in [first, data_store_id("work")] {
            assert_eq!(uuid[6] >> 4, 8, "a version 8 UUID");
            assert_eq!(uuid[8] >> 6, 0b10, "the RFC variant");
        }
    }

    #[test]
    fn sweeps_only_the_folders_of_profiles_that_are_gone() {
        let state = add(&base(), "work", "Work");
        let names: Vec<String> = ["default", "work", "gone", "Notes", ".DS_Store", "-x"]
            .iter()
            .map(|name| name.to_string())
            .collect();
        assert_eq!(orphaned_profile_dirs(&names, &state), ["gone"]);
        assert!(orphaned_profile_dirs(&[], &state).is_empty());
    }

    #[test]
    fn sweeps_only_the_stores_of_profiles_that_are_gone() {
        let state = add(&base(), "work", "Work");
        let gone = data_store_id("gone");
        let [panel, gone_panel] =
            [panel_data_dir("work"), panel_data_dir("gone")].map(|name| data_store_id(&name));
        let stores = [
            data_store_id("work"),
            gone,
            data_store_id(FIRST_PROFILE_ID),
            panel,
            gone_panel,
        ];
        assert_eq!(orphaned_data_stores(&stores, &state), [gone, gone_panel]);
        assert!(orphaned_data_stores(&[], &state).is_empty());
        assert_eq!(profile_stores("work"), [data_store_id("work"), panel]);
    }
}
