# Jira

Send a Jira Cloud issue to AgentRQ, and an agent gets a task with the issue's details. Once set up, you open an issue assigned to you and pick **Actions → Send to AgentRQ**, and a task shows up in your workspace.

It works through the built-in `jira` [hook](HOOKS.md). It takes five steps, the first one once per server:

| Step | Who | Where |
|---|---|---|
| 1. Add the `jira` hook to the server | the server's operator | AgentRQ admin page |
| 2. Find your Jira account id | you | Jira |
| 3. Make an event, and a trigger that creates the task | you | AgentRQ, **Events** |
| 4. Register the hook | you | AgentRQ, **Events → Hooks** |
| 5. Make the "Send to AgentRQ" rule | a Jira project admin | Jira, **Project settings → Automation** |

## 1. Add the `jira` hook to the server

If **jira** is already listed when you choose **Events → Hooks → + New registration**, skip this step. Otherwise the server's operator adds it once:

1. Download `agentrq-hook-jira_<version>.wasm` from the server's release on GitHub.
2. Sign in at `<AGENTRQ_BASE_URL>/admin/login` with the root access token.
3. On the **Hooks** tab, upload the file.

[SETUP_PRODUCTION.md](SETUP_PRODUCTION.md#hooks) shows how to do the same from a script. Jira must be able to reach the server: `AGENTRQ_BASE_URL` has to be a public `https://` address, and a proxy in front must pass `POST /api/v1/hooks/in/` through.

## 2. Find your Jira account id

In Jira, open your profile: your avatar, then **Profile**. The address ends in your account id: `https://<site>.atlassian.net/jira/people/<account id>`, for example `5b10ac8d82e05b22cc7d4ef5`.

The hook matches people by account id only. A display name can be shared by two people and changed by anybody, and Jira Cloud does not send email addresses.

## 3. Make an event and its trigger

1. In AgentRQ, open **Events** and choose **+ New Event**. Name it, for example `jira-issue`.
2. Open the event and choose **+ Add Trigger**:
   - **Target Workspace**: the workspace whose agent should do the work.
   - **Task Title**: for example `Jira issue`. Every task gets this title; the issue's own summary is in the body.
   - **Task Instructions**: what the agent should do, with `{{EVENT_PAYLOAD}}` where the issue goes. For example:

     ```
     Work on this Jira issue, then open a pull request.

     {{EVENT_PAYLOAD}}
     ```

## 4. Register the hook

1. Open **Events → Hooks** and choose **+ New registration**.
2. Pick **jira** and name the registration, for example `Jira ABC`.
3. Fill in its inputs:

   | Input | Value |
   |---|---|
   | **Secret** (required) | choose **Generate**, and copy it now: it is never shown again |
   | **Assignees** (required) | your account id from step 2. The issue must be assigned to one of these people |
   | **Actors** (required) | your account id again. One of these people must be the one who ran the rule, so a teammate running it on your issue starts nothing |
   | Project keys | optional, such as `ABC` |
   | Labels | optional; the issue must have one, such as `agentrq` |

   Separate several account ids with commas. For a team's shared workspace, list everybody's in both.

4. Route **jira.issue.sent** to the event from step 3. Leave the others **Not routed** unless you also set up the webhook below.
5. Choose **Create**, and copy the **Payload URL** the page now shows. It is shown in full only this once. If you lose it, **Rotate** makes a new one.

## 5. Make the "Send to AgentRQ" rule in Jira

A Jira project admin (or Jira admin) does this:

1. Open the project's **Project settings → Automation**, and choose **Create rule**.
2. Trigger: **Manual trigger from work item**. Under **Groups that can run trigger**, you can limit who sees it.
3. Action: **Send web request**:

   | Field | Value |
   |---|---|
   | Web request URL | the Payload URL from step 4 |
   | HTTP method | `POST` |
   | Web request body | **Work item data (Jira format)**. Not **(Automation format)**: it does not say who ran the rule, so **Actors** never matches |
   | Headers | name `Authorization`, value `Bearer <Secret from step 4>`, and tick **Hidden** |

4. Name the rule **Send to AgentRQ**, and turn it on.

A rule sends to one Payload URL, which belongs to one person's registration. Each person who wants their own tasks needs their own rule, or another **Send web request** action, with their URL and secret, in a shared rule.

## Try it

Open an issue assigned to you, and choose **Actions → Send to AgentRQ**. Within a few seconds, a task titled as in step 3 appears in the workspace.

If none appears, open the registration in **Events → Hooks** and look at its **Deliveries**:

| What you see | Why |
|---|---|
| No delivery at all | Jira did not reach the server: check the rule's audit log in Jira, and that `AGENTRQ_BASE_URL` is reachable from the internet. |
| `rejected` "neither an X-Hub-Signature nor an Authorization: Bearer header…" | The rule's `Authorization` header is missing, or its secret is not the registration's. |
| `rejected` "the registration's assignees is empty…" (or actors) | Fill in **Assignees** and **Actors**. |
| `ignored` | The issue is not assigned to one of your **Assignees**, it was not one of your **Actors** who ran the rule, the body is not **Jira format**, or another filter left it out. |
| `published`, but no task | The event has no trigger, or **jira.issue.sent** is not routed to it. |

## Other ways to send issues

The same rule can run on other triggers than a click, such as **Work item transitioned** to a status, or a label being added. Keep the conditions narrow: every request Jira sends is stored and checked, even one the filters then leave out.

A Jira admin can also add a webhook, in **Jira settings → System → WebHooks**, with the Payload URL, the registration's **Secret**, and a JQL filter such as `project = ABC AND assignee = 5b10ac8d82e05b22cc7d4ef5`. It sends `jira.issue.created`, `jira.issue.updated` and `jira.comment.created`, which you route in the registration like `jira.issue.sent`. Here the actor is whoever created or changed the issue, or wrote the comment. Jira signs these requests itself, so no `Authorization` header is needed.

The hook only reads from Jira. For the agent to comment on the issue or move it when done, give the workspace access to Jira, such as the Atlassian MCP server, and say so in the trigger's instructions.
