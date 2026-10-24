// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//! Who is signed in to a profile.
//!
//! The switcher shows a name and email rather than only a label, because a list
//! of profiles named "Default" and "Work" does not tell you which account you
//! are about to switch to.
//!
//! Each lookup runs against that profile's own session, so it answers for that
//! profile rather than for whichever one is on screen. Everything is best
//! effort: a profile that is signed out, pointed at a server that is down, or
//! not configured yet is a normal state, not an error.
//!
//! Rust notes: the JS gives `fetch` an `AbortSignal.timeout`. Here the request
//! is a trait and the four-second deadline is a second one, [`Deadline`], that
//! races the request, so the deadline can be tested without waiting for it.

use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::sync::mpsc;
use std::sync::Arc;
use std::thread;
use std::time::Duration;

/// How long to wait before deciding a profile cannot answer for itself.
pub const IDENTITY_TIMEOUT_MS: u64 = 4000;

/// The user endpoint, unauthenticated-safe: it 401s rather than erroring.
pub const USER_PATH: &str = "/api/v1/auth/user";

/// What the switcher shows about a signed-in user.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Account {
    pub name: String,
    pub email: String,
    pub picture: String,
}

impl Account {
    /// The key two profiles are the same account under. See [`account_key`].
    pub fn key(&self) -> String {
        self.email.trim().to_lowercase()
    }
}

/// What a profile's session answered.
#[derive(Debug, Clone, PartialEq)]
pub struct UserReply {
    /// The HTTP status was a success.
    pub ok: bool,
    /// The body as JSON; `Err` when it was not.
    pub json: Result<Value, String>,
}

/// One profile's session `fetch`, for the user endpoint only.
pub trait UserFetch: Send + Sync {
    /// `Err` is a transport failure (refused, reset); `Ok(None)` is a fetch
    /// that answered with nothing at all.
    fn get(&self, url: &str) -> Result<Option<UserReply>, String>;
}

/// The work handed to a [`Deadline`].
pub type UserWork = Box<dyn FnOnce() -> Result<Option<UserReply>, String> + Send + 'static>;

/// Races a request against a clock.
pub trait Deadline {
    /// Run `work` and return its result, or `None` if `ms` milliseconds pass
    /// first. The work is abandoned, not cancelled: its answer is dropped.
    fn race(&self, ms: u64, work: UserWork) -> Option<Result<Option<UserReply>, String>>;
}

/// The real clock: the work runs on its own thread, and the caller waits for it
/// for at most the deadline.
pub struct ThreadDeadline;

impl Deadline for ThreadDeadline {
    fn race(&self, ms: u64, work: UserWork) -> Option<Result<Option<UserReply>, String>> {
        let (tx, rx) = mpsc::channel();
        thread::spawn(move || {
            // The receiver is gone once the deadline has passed.
            let _ = tx.send(work());
        });
        rx.recv_timeout(Duration::from_millis(ms)).ok()
    }
}

/// Ask one profile who it is signed in as.
///
/// `server_url` is that profile's server; `""` means unconfigured. Everything
/// that goes wrong is `None`: unreachable, refused, timed out, not signed in,
/// not JSON.
pub fn fetch_profile_identity(
    fetch: Arc<dyn UserFetch>,
    server_url: &str,
    deadline: &dyn Deadline,
) -> Option<Account> {
    if server_url.is_empty() {
        return None;
    }

    let url = format!("{server_url}{USER_PATH}");
    let reply = deadline
        .race(IDENTITY_TIMEOUT_MS, Box::new(move || fetch.get(&url)))
        .and_then(Result::ok)
        .flatten()?;

    // 401 is the ordinary answer for a profile nobody has signed into yet.
    if !reply.ok {
        return None;
    }
    describe_identity(&reply.json.ok()?)
}

/// The key two profiles are the same account under.
///
/// The email, because it is the one thing an account cannot have two of and a
/// person cannot have two spellings of: names collide between colleagues and
/// change when somebody marries, and the picture is a URL that rotates.
/// Lowercased and trimmed.
///
/// Empty when there is no email, which is never equal to anything: two profiles
/// we cannot identify are not a duplicate.
pub fn account_key(account: &Value) -> String {
    account
        .get("email")
        .and_then(Value::as_str)
        .map(|email| email.trim().to_lowercase())
        .unwrap_or_default()
}

/// Reduce a user payload to what the switcher shows. `None` when there is
/// nothing worth showing, so callers have one thing to check.
pub fn describe_identity(user: &Value) -> Option<Account> {
    if !user.is_object() {
        return None;
    }
    let text = |key: &str| user.get(key).and_then(Value::as_str);

    let name = text("name").unwrap_or("").trim().to_string();
    let email = text("email").unwrap_or("").trim().to_string();
    let picture = text("picture").unwrap_or("").to_string();

    // A response with neither is indistinguishable from being signed out, as
    // far as anything the user can see goes.
    if name.is_empty() && email.is_empty() {
        return None;
    }
    Some(Account {
        name,
        email,
        picture,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::cell::RefCell;
    use std::sync::Mutex;

    fn ok(body: Value) -> Result<Option<UserReply>, String> {
        Ok(Some(UserReply {
            ok: true,
            json: Ok(body),
        }))
    }

    /// A `fetch` that records what it was asked and answers with a canned reply.
    struct FakeFetch {
        reply: Result<Option<UserReply>, String>,
        urls: Mutex<Vec<String>>,
    }

    impl FakeFetch {
        fn answering(reply: Result<Option<UserReply>, String>) -> Arc<Self> {
            Arc::new(Self {
                reply,
                urls: Mutex::new(Vec::new()),
            })
        }
        fn urls(&self) -> Vec<String> {
            self.urls.lock().unwrap().clone()
        }
    }

    impl UserFetch for FakeFetch {
        fn get(&self, url: &str) -> Result<Option<UserReply>, String> {
            self.urls.lock().unwrap().push(url.to_string());
            self.reply.clone()
        }
    }

    /// A clock that runs the work at once, or never lets it finish.
    struct FakeDeadline {
        expires: bool,
        asked: RefCell<Vec<u64>>,
    }

    impl FakeDeadline {
        fn on_time() -> Self {
            Self {
                expires: false,
                asked: RefCell::new(Vec::new()),
            }
        }
        fn already_late() -> Self {
            Self {
                expires: true,
                asked: RefCell::new(Vec::new()),
            }
        }
    }

    impl Deadline for FakeDeadline {
        fn race(&self, ms: u64, work: UserWork) -> Option<Result<Option<UserReply>, String>> {
            self.asked.borrow_mut().push(ms);
            if self.expires {
                None
            } else {
                Some(work())
            }
        }
    }

    fn lookup(fetch: &Arc<FakeFetch>, server_url: &str) -> Option<Account> {
        fetch_profile_identity(fetch.clone(), server_url, &FakeDeadline::on_time())
    }

    fn account(name: &str, email: &str, picture: &str) -> Account {
        Account {
            name: name.into(),
            email: email.into(),
            picture: picture.into(),
        }
    }

    // fetchProfileIdentity

    #[test]
    fn reports_who_a_profile_is_signed_in_as() {
        let fetch = FakeFetch::answering(ok(
            json!({"name": "Ada", "email": "ada@example.com", "picture": "p.png"}),
        ));

        let identity = lookup(&fetch, "https://a.test");

        assert_eq!(identity, Some(account("Ada", "ada@example.com", "p.png")));
        assert_eq!(fetch.urls(), [format!("https://a.test{USER_PATH}")]);
    }

    #[test]
    fn asks_the_profiles_own_server_not_a_shared_one() {
        let fetch = FakeFetch::answering(ok(json!({"email": "b@example.com"})));

        lookup(&fetch, "https://b.test");

        assert_eq!(fetch.urls(), [format!("https://b.test{USER_PATH}")]);
    }

    #[test]
    fn says_nothing_for_a_profile_that_has_no_server_yet() {
        // A freshly added profile, still on the connection screen.
        let fetch = FakeFetch::answering(ok(json!({})));
        let deadline = FakeDeadline::on_time();

        assert_eq!(fetch_profile_identity(fetch.clone(), "", &deadline), None);
        assert!(fetch.urls().is_empty());
        assert!(deadline.asked.borrow().is_empty());
    }

    #[test]
    fn treats_a_signed_out_profile_as_ordinary_not_an_error() {
        let fetch = FakeFetch::answering(Ok(Some(UserReply {
            ok: false,
            json: Ok(Value::Null),
        })));

        assert_eq!(lookup(&fetch, "https://a.test"), None);
    }

    #[test]
    fn survives_a_server_that_cannot_be_reached() {
        let fetch = FakeFetch::answering(Err("ECONNREFUSED".into()));

        assert_eq!(lookup(&fetch, "https://a.test"), None);
    }

    #[test]
    fn survives_a_response_that_is_not_json() {
        let fetch = FakeFetch::answering(Ok(Some(UserReply {
            ok: true,
            json: Err("not json".into()),
        })));

        assert_eq!(lookup(&fetch, "https://a.test"), None);
    }

    #[test]
    fn survives_a_fetch_that_answers_with_nothing_at_all() {
        let fetch = FakeFetch::answering(Ok(None));

        assert_eq!(lookup(&fetch, "https://a.test"), None);
    }

    #[test]
    fn gives_up_rather_than_letting_one_profile_hold_up_the_menu() {
        let fetch = FakeFetch::answering(ok(json!({"email": "a@example.com"})));
        let deadline = FakeDeadline::on_time();

        let identity = fetch_profile_identity(fetch.clone(), "https://a.test", &deadline);

        assert_eq!(*deadline.asked.borrow(), [IDENTITY_TIMEOUT_MS]);
        assert_eq!(fetch.urls(), [format!("https://a.test{USER_PATH}")]);
        assert_eq!(identity, Some(account("", "a@example.com", "")));
    }

    #[test]
    fn answers_nothing_when_the_deadline_passes_first() {
        // Not in the vitest file: the JS aborts the fetch, here the answer is
        // dropped, and either way the profile reads as "cannot say".
        let fetch = FakeFetch::answering(ok(json!({"email": "a@example.com"})));

        let identity = fetch_profile_identity(
            fetch.clone(),
            "https://a.test",
            &FakeDeadline::already_late(),
        );

        assert_eq!(identity, None);
        assert!(fetch.urls().is_empty());
    }

    struct SlowFetch(Duration);

    impl UserFetch for SlowFetch {
        fn get(&self, _url: &str) -> Result<Option<UserReply>, String> {
            thread::sleep(self.0);
            Ok(None)
        }
    }

    #[test]
    fn the_real_clock_returns_the_answer_when_it_comes_in_time() {
        // Not in the vitest file: `ThreadDeadline` has no JS counterpart.
        let fetch = FakeFetch::answering(ok(json!({"name": "Ada"})));
        let work: UserWork = Box::new(move || fetch.get("https://a.test"));

        let answer = ThreadDeadline.race(5000, work);

        assert_eq!(answer, Some(ok(json!({"name": "Ada"}))));
    }

    #[test]
    fn the_real_clock_stops_waiting_when_the_deadline_passes() {
        let slow = SlowFetch(Duration::from_millis(500));
        let work: UserWork = Box::new(move || slow.get("https://a.test"));

        assert_eq!(ThreadDeadline.race(10, work), None);
    }

    // describeIdentity

    #[test]
    fn keeps_only_what_the_switcher_shows() {
        assert_eq!(
            describe_identity(&json!({
                "name": " Ada ", "email": " ada@example.com ", "picture": "p.png", "token": "secret"
            })),
            Some(account("Ada", "ada@example.com", "p.png"))
        );
    }

    #[test]
    fn accepts_a_user_with_only_one_of_the_two() {
        assert_eq!(
            describe_identity(&json!({"email": "a@example.com"})),
            Some(account("", "a@example.com", ""))
        );
        assert_eq!(
            describe_identity(&json!({"name": "Ada"})),
            Some(account("Ada", "", ""))
        );
    }

    #[test]
    fn is_null_when_there_is_nothing_worth_showing() {
        // Indistinguishable from signed out, as far as anyone can see.
        for user in [
            json!(null),
            json!("a string"),
            json!(42),
            json!({}),
            json!({"name": "  ", "email": ""}),
            json!({"name": 7, "email": {}}),
        ] {
            assert_eq!(describe_identity(&user), None, "{user}");
        }
    }

    // accountKey

    #[test]
    fn is_the_address_however_it_was_typed() {
        assert_eq!(
            account_key(&json!({"email": "  Sam@Example.COM "})),
            "sam@example.com"
        );
        assert_eq!(
            account_key(&json!({"name": "Sam", "email": "sam@example.com"})),
            "sam@example.com"
        );
        assert_eq!(
            account("Sam", " Sam@Example.COM ", "").key(),
            "sam@example.com"
        );
    }

    #[test]
    fn is_nothing_when_there_is_no_address() {
        for nothing in [
            json!(null),
            json!({}),
            json!({"name": "Sam"}),
            json!({"email": "   "}),
            json!({"email": 42}),
        ] {
            assert_eq!(account_key(&nothing), "", "{nothing}");
        }
    }
}
