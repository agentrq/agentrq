// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Tray / menu-bar item.
//!
//! The point of a tray item is to answer "is anything waiting for me?" without
//! switching to the app, and to get somewhere in one click when the answer is
//! yes. So it carries the unread count and the workspaces the user has heard
//! from most recently.
//!
//! Built as plain data, so the contents are testable without a tray. Only the
//! decision of *what to show* lives here: the icon itself, and the OS call that
//! draws it, are behind [`Tray`].
//!
//! Rust notes: a menu item's `click` is data, a [`TrayAction`], rather than a
//! closure; [`dispatch`] is what runs one, so "wires each action" is a value
//! comparison. The JS `actions.openWorkspace?.()` has no counterpart because
//! the item always carries the action and the host decides what to do with it.

/// Most recent workspaces to offer. Enough to be useful, short enough to scan.
pub const MAX_RECENT_WORKSPACES: usize = 5;

/// A workspace the user has heard from recently.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecentWorkspace {
    pub id: String,
    pub name: String,
}

/// Track which workspaces have been active most recently.
///
/// Ordered by last activity rather than alphabetically: the tray is a shortcut
/// to what is happening now, and a fixed list would make it a worse version of
/// the sidebar.
#[derive(Debug, Clone)]
pub struct RecentWorkspaces {
    limit: usize,
    recent: Vec<RecentWorkspace>,
}

impl Default for RecentWorkspaces {
    fn default() -> Self {
        Self::with_limit(MAX_RECENT_WORKSPACES)
    }
}

impl RecentWorkspaces {
    pub fn with_limit(limit: usize) -> Self {
        Self {
            limit,
            recent: Vec::new(),
        }
    }

    /// Record activity, moving the workspace to the front.
    pub fn touch(&mut self, id: &str, name: Option<&str>) -> &[RecentWorkspace] {
        if id.is_empty() {
            return &self.recent;
        }
        self.recent.retain(|entry| entry.id != id);
        self.recent.insert(
            0,
            RecentWorkspace {
                id: id.to_string(),
                name: name.unwrap_or_default().to_string(),
            },
        );
        self.recent.truncate(self.limit);
        &self.recent
    }

    /// Fill in names that were unknown when the activity was recorded.
    pub fn rename(&mut self, id: &str, name: &str) -> &[RecentWorkspace] {
        for entry in self.recent.iter_mut().filter(|entry| entry.id == id) {
            entry.name = name.to_string();
        }
        &self.recent
    }

    pub fn list(&self) -> &[RecentWorkspace] {
        &self.recent
    }
}

/// What choosing a menu item does.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TrayAction {
    Open,
    NewTask,
    OpenWorkspace(String),
    Quit,
}

/// One row of the tray menu.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TrayMenuItem {
    Separator,
    Item {
        id: String,
        label: String,
        enabled: bool,
        action: Option<TrayAction>,
    },
}

impl TrayMenuItem {
    fn label(id: &str, label: &str) -> Self {
        Self::Item {
            id: id.to_string(),
            label: label.to_string(),
            enabled: false,
            action: None,
        }
    }

    fn action(id: &str, label: &str, action: TrayAction) -> Self {
        Self::Item {
            id: id.to_string(),
            label: label.to_string(),
            enabled: true,
            action: Some(action),
        }
    }

    /// The row's text, which a separator does not have.
    pub fn text(&self) -> Option<&str> {
        match self {
            Self::Separator => None,
            Self::Item { label, .. } => Some(label),
        }
    }

    /// What choosing the row does, if anything.
    pub fn chosen(&self) -> Option<&TrayAction> {
        match self {
            Self::Item { action, .. } => action.as_ref(),
            Self::Separator => None,
        }
    }

    /// The row's id, which a separator does not have.
    pub fn id(&self) -> Option<&str> {
        match self {
            Self::Separator => None,
            Self::Item { id, .. } => Some(id),
        }
    }
}

/// The rows of `template` with this id.
pub fn find_menu_items<'a>(template: &'a [TrayMenuItem], id: &str) -> Vec<&'a TrayMenuItem> {
    template
        .iter()
        .filter(|item| item.id() == Some(id))
        .collect()
}

/// Tray menu contents.
pub fn build_tray_menu_template(
    workspaces: &[RecentWorkspace],
    unread_count: i64,
) -> Vec<TrayMenuItem> {
    let status = if unread_count > 0 {
        // The count is the whole reason to glance at the tray, so it leads.
        format!("{unread_count} unread")
    } else {
        "No unread notifications".to_string()
    };
    let mut template = vec![
        TrayMenuItem::label("status", &status),
        TrayMenuItem::Separator,
        TrayMenuItem::action("open", "Open AgentRQ", TrayAction::Open),
        TrayMenuItem::action("new-task", "New Task…", TrayAction::NewTask),
    ];

    if !workspaces.is_empty() {
        template.push(TrayMenuItem::Separator);
        template.push(TrayMenuItem::label("recent-heading", "Recent Workspaces"));
        for workspace in workspaces {
            template.push(TrayMenuItem::action(
                &format!("workspace-{}", workspace.id),
                // A workspace whose name has not been resolved yet still
                // deserves a usable entry rather than a blank line.
                if workspace.name.is_empty() {
                    "Untitled workspace"
                } else {
                    &workspace.name
                },
                TrayAction::OpenWorkspace(workspace.id.clone()),
            ));
        }
    }

    template.push(TrayMenuItem::Separator);
    template.push(TrayMenuItem::action(
        "quit",
        "Quit AgentRQ",
        TrayAction::Quit,
    ));
    template
}

/// Tooltip shown on hover.
pub fn tray_tooltip(unread_count: i64) -> String {
    if unread_count <= 0 {
        return "AgentRQ".to_string();
    }
    let plural = if unread_count == 1 { "" } else { "s" };
    format!("AgentRQ — {unread_count} unread notification{plural}")
}

/// The OS tray, implemented by the shell.
pub trait Tray {
    fn set_menu(&mut self, template: &[TrayMenuItem]);
    fn set_tooltip(&mut self, tooltip: &str);
}

/// What the shell does when a menu item is chosen.
pub trait TrayActions {
    fn open(&mut self);
    fn new_task(&mut self);
    fn open_workspace(&mut self, id: &str);
    fn quit(&mut self);
}

/// Show the current state on the tray.
pub fn update_tray(tray: &mut impl Tray, workspaces: &[RecentWorkspace], unread_count: i64) {
    tray.set_menu(&build_tray_menu_template(workspaces, unread_count));
    tray.set_tooltip(&tray_tooltip(unread_count));
}

/// Run what choosing an item means.
pub fn dispatch(action: &TrayAction, actions: &mut impl TrayActions) {
    match action {
        TrayAction::Open => actions.open(),
        TrayAction::NewTask => actions.new_task(),
        TrayAction::OpenWorkspace(id) => actions.open_workspace(id),
        TrayAction::Quit => actions.quit(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ids(recent: &RecentWorkspaces) -> Vec<&str> {
        recent.list().iter().map(|w| w.id.as_str()).collect()
    }

    fn ws(id: &str, name: &str) -> RecentWorkspace {
        RecentWorkspace {
            id: id.to_string(),
            name: name.to_string(),
        }
    }

    fn label_of(template: &[TrayMenuItem], id: &str) -> String {
        find_menu_items(template, id)[0].text().unwrap().to_string()
    }

    fn action_of(template: &[TrayMenuItem], id: &str) -> Option<TrayAction> {
        find_menu_items(template, id)[0].chosen().cloned()
    }

    // createRecentWorkspaces

    #[test]
    fn records_activity() {
        let mut recent = RecentWorkspaces::default();
        assert_eq!(recent.touch("ws1", Some("Alpha")), [ws("ws1", "Alpha")]);
    }

    #[test]
    fn orders_by_most_recent_not_by_name() {
        // The tray is a shortcut to what is happening now; a fixed order would
        // make it a worse version of the sidebar.
        let mut recent = RecentWorkspaces::default();
        recent.touch("ws1", Some("Alpha"));
        recent.touch("ws2", Some("Beta"));

        assert_eq!(ids(&recent), ["ws2", "ws1"]);
    }

    #[test]
    fn moves_a_repeat_visitor_to_the_front_without_duplicating_it() {
        let mut recent = RecentWorkspaces::default();
        recent.touch("ws1", Some("Alpha"));
        recent.touch("ws2", Some("Beta"));
        recent.touch("ws1", Some("Alpha"));

        assert_eq!(ids(&recent), ["ws1", "ws2"]);
    }

    #[test]
    fn keeps_the_list_short_enough_to_scan() {
        let mut recent = RecentWorkspaces::default();
        for i in 0..12 {
            recent.touch(&format!("ws{i}"), Some(&format!("W{i}")));
        }

        assert_eq!(recent.list().len(), MAX_RECENT_WORKSPACES);
        assert_eq!(recent.list()[0].id, "ws11");
    }

    #[test]
    fn honours_a_custom_limit() {
        let mut recent = RecentWorkspaces::with_limit(2);
        recent.touch("a", None);
        recent.touch("b", None);
        recent.touch("c", None);

        assert_eq!(ids(&recent), ["c", "b"]);
    }

    #[test]
    fn ignores_activity_with_no_workspace() {
        let mut recent = RecentWorkspaces::default();
        assert_eq!(recent.touch("", Some("Nameless")), []);
    }

    #[test]
    fn fills_in_a_name_that_was_unknown_at_the_time() {
        // Activity can arrive before the workspace list has been fetched.
        let mut recent = RecentWorkspaces::default();
        recent.touch("ws1", None);

        assert_eq!(recent.rename("ws1", "Alpha"), [ws("ws1", "Alpha")]);
        assert_eq!(recent.rename("unknown", "x"), [ws("ws1", "Alpha")]);
    }

    #[test]
    fn stores_an_empty_name_rather_than_undefined() {
        let mut recent = RecentWorkspaces::default();
        assert_eq!(recent.touch("ws1", None), [ws("ws1", "")]);
    }

    // buildTrayMenuTemplate

    #[test]
    fn leads_with_the_unread_count_which_is_why_anyone_looks() {
        assert_eq!(
            build_tray_menu_template(&[], 3)[0],
            TrayMenuItem::Item {
                id: "status".to_string(),
                label: "3 unread".to_string(),
                enabled: false,
                action: None,
            }
        );
    }

    #[test]
    fn says_so_plainly_when_there_is_nothing_waiting() {
        assert_eq!(
            label_of(&build_tray_menu_template(&[], 0), "status"),
            "No unread notifications"
        );
    }

    #[test]
    fn always_offers_open_new_task_and_quit() {
        let template = build_tray_menu_template(&[], 0);

        for id in ["open", "new-task", "quit"] {
            assert_eq!(find_menu_items(&template, id).len(), 1);
        }
    }

    #[test]
    fn lists_recent_workspaces_when_there_are_any() {
        let template = build_tray_menu_template(&[ws("ws1", "Alpha"), ws("ws2", "Beta")], 0);

        assert_eq!(label_of(&template, "workspace-ws1"), "Alpha");
        assert_eq!(label_of(&template, "workspace-ws2"), "Beta");
        assert_eq!(find_menu_items(&template, "recent-heading").len(), 1);
    }

    #[test]
    fn omits_the_recent_section_entirely_when_there_is_nothing_to_list() {
        let template = build_tray_menu_template(&[], 0);
        assert!(find_menu_items(&template, "recent-heading").is_empty());
    }

    #[test]
    fn gives_an_unnamed_workspace_a_usable_label_rather_than_a_blank_line() {
        let template = build_tray_menu_template(&[ws("ws1", "")], 0);
        assert_eq!(label_of(&template, "workspace-ws1"), "Untitled workspace");
    }

    #[derive(Default)]
    struct Calls(Vec<String>);

    impl TrayActions for Calls {
        fn open(&mut self) {
            self.0.push("open".to_string());
        }
        fn new_task(&mut self) {
            self.0.push("new-task".to_string());
        }
        fn open_workspace(&mut self, id: &str) {
            self.0.push(format!("workspace:{id}"));
        }
        fn quit(&mut self) {
            self.0.push("quit".to_string());
        }
    }

    #[test]
    fn wires_each_action() {
        let template = build_tray_menu_template(&[ws("ws1", "Alpha")], 0);
        let mut calls = Calls::default();

        for id in ["open", "new-task", "workspace-ws1", "quit"] {
            dispatch(&action_of(&template, id).unwrap(), &mut calls);
        }

        assert_eq!(calls.0, ["open", "new-task", "workspace:ws1", "quit"]);
    }

    #[test]
    fn does_not_throw_when_no_actions_are_supplied() {
        // Items carry their action as data, so there is no handler to be
        // missing when the menu is built; the workspace entry is still wired.
        let template = build_tray_menu_template(&[ws("ws1", "Alpha")], 0);
        assert_eq!(
            action_of(&template, "workspace-ws1"),
            Some(TrayAction::OpenWorkspace("ws1".to_string()))
        );
        assert_eq!(action_of(&template, "status"), None);
        assert_eq!(TrayMenuItem::Separator.id(), None);
        assert_eq!(TrayMenuItem::Separator.text(), None);
        assert_eq!(TrayMenuItem::Separator.chosen(), None);
    }

    // trayTooltip

    #[test]
    fn is_just_the_name_when_nothing_is_waiting() {
        assert_eq!(tray_tooltip(0), "AgentRQ");
        assert_eq!(tray_tooltip(-2), "AgentRQ");
    }

    #[test]
    fn carries_the_count_when_something_is() {
        assert_eq!(tray_tooltip(4), "AgentRQ — 4 unread notifications");
    }

    #[test]
    fn gets_the_singular_right() {
        assert_eq!(tray_tooltip(1), "AgentRQ — 1 unread notification");
    }

    // The OS tray, behind a trait.

    #[derive(Default)]
    struct FakeTray {
        menu: Vec<TrayMenuItem>,
        tooltip: String,
    }

    impl Tray for FakeTray {
        fn set_menu(&mut self, template: &[TrayMenuItem]) {
            self.menu = template.to_vec();
        }
        fn set_tooltip(&mut self, tooltip: &str) {
            self.tooltip = tooltip.to_string();
        }
    }

    #[test]
    fn update_tray_shows_the_menu_and_the_tooltip_together() {
        let mut tray = FakeTray::default();
        update_tray(&mut tray, &[ws("ws1", "Alpha")], 2);

        assert_eq!(label_of(&tray.menu, "status"), "2 unread");
        assert_eq!(tray.tooltip, "AgentRQ — 2 unread notifications");
    }
}
