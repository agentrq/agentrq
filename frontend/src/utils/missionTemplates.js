// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { DEFAULT_WORKSPACE_MISSION, WORKSPACE_WORKING_RULES } from './workspaceForm';

/**
 * Suggested missions, by category and then by speciality. They only pre-fill
 * the mission field of the create and settings forms; nothing about a
 * category is stored, so the text stays the person's to edit.
 */
const CATALOGUE = [
  {
    id: 'general',
    label: 'General',
    subcategories: [
      // The form's own default, so a new workspace starts with General chosen.
      { id: 'general', label: 'General Purpose', mission: DEFAULT_WORKSPACE_MISSION },
    ],
  },
  {
    id: 'coding',
    label: 'Coding',
    // Appended below the working rules of every coding mission.
    closing: `**Pull requests**
Every PR or diff description carries:
- What: what changed
- Why: the reason for it
- Test coverage: target 100% unit test coverage on the new lines introduced
- AgentRQ Task ID: <task ID>`,
    subcategories: [
      {
        id: 'backend',
        label: 'Backend',
        role: 'backend development',
        focus: [
          'Design APIs and data models that are simple to call and hard to misuse.',
          'Cover every change with tests, and run the suite before calling it done.',
          'Keep migrations reversible and never break a running client.',
        ],
      },
      {
        id: 'frontend',
        label: 'Frontend',
        role: 'frontend development',
        focus: [
          'Build accessible, responsive screens that work on phones and desktops, in light and dark themes.',
          'Reuse the existing components and styles before adding new ones.',
          'Share a screenshot of every visible change.',
        ],
      },
      {
        id: 'full-stack',
        label: 'Full Stack',
        role: 'full-stack development',
        focus: [
          'Change the API, the data and the screens together, so no layer is left behind.',
          'Cover every change with tests on both sides, and run them before calling it done.',
          'Share a screenshot of every visible change.',
        ],
      },
      {
        id: 'ios',
        label: 'iOS',
        role: 'iOS app development',
        focus: [
          'Follow the platform conventions and keep the app smooth on older devices.',
          'Test on the simulator and on a real device before a release.',
          'Mind App Store review rules when adding permissions or payments.',
        ],
      },
      {
        id: 'android',
        label: 'Android',
        role: 'Android app development',
        focus: [
          'Follow Material conventions and support the range of screen sizes and OS versions the app targets.',
          'Test on an emulator and a real device before a release.',
          'Mind Play Store policies when adding permissions or payments.',
        ],
      },
      {
        id: 'testing',
        label: 'Testing',
        role: 'testing and quality assurance',
        focus: [
          'Write tests that fail for the bug before they pass for the fix.',
          'Report each defect with steps to reproduce, expected and actual results.',
          'Keep the suite fast and free of flaky tests.',
        ],
      },
    ],
  },
  {
    id: 'manager',
    label: 'Manager',
    subcategories: [
      {
        id: 'pm',
        label: 'PM',
        role: 'product management',
        focus: [
          'Write specs that state the problem, who has it and how success is measured, before the solution.',
          'Keep the roadmap and backlog prioritised, with the reason for each priority.',
          'Turn customer feedback and usage data into decisions, and record them.',
        ],
      },
      {
        id: 'em',
        label: 'EM',
        role: 'engineering management',
        focus: [
          'Keep the team\'s work, owners and blockers visible, and chase what is stuck.',
          'Prepare one-on-one notes, review agendas and growth plans for each engineer.',
          'Treat feedback and performance notes as confidential, and share them with no one without the human.',
        ],
      },
      {
        id: 'tpm',
        label: 'TPM',
        role: 'technical program management',
        focus: [
          'Map work across teams with its dependencies, milestones and owners.',
          'Keep a risk register, and raise a slipping date as soon as it slips.',
          'Send a short status update each week: done, next, at risk.',
        ],
      },
      {
        id: 'project',
        label: 'Project',
        role: 'project management',
        focus: [
          'Break the project into tasks with owners, estimates and deadlines.',
          'Track progress against the plan, and flag scope changes before they land.',
          'Keep meeting notes, decisions and action items in one place.',
        ],
      },
      {
        id: 'release',
        label: 'Release',
        role: 'release management',
        focus: [
          'Keep a release calendar, and a checklist for every release.',
          'Write release notes in plain language from what actually merged.',
          'Ship nothing without the human\'s go-ahead, and keep a rollback plan ready.',
        ],
      },
    ],
  },
  {
    id: 'sales',
    label: 'Sales',
    subcategories: [
      {
        id: 'prospecting',
        label: 'Prospecting',
        role: 'finding and qualifying leads',
        focus: [
          'Research companies and contacts that fit the ideal customer profile.',
          'Record every lead with its source and why it fits.',
          'Never contact anyone without the human approving the list first.',
        ],
      },
      {
        id: 'outreach',
        label: 'Outreach',
        role: 'sales outreach',
        focus: [
          'Draft short, personal messages that lead with the prospect\'s problem.',
          'Plan follow-ups and track replies.',
          'Send nothing on the human\'s behalf until they approve the draft.',
        ],
      },
      {
        id: 'crm',
        label: 'CRM',
        role: 'keeping the sales pipeline up to date',
        focus: [
          'Keep every deal\'s stage, next step and owner current.',
          'Flag deals that have gone quiet.',
          'Summarise the pipeline each week.',
        ],
      },
    ],
  },
  {
    id: 'marketing',
    label: 'Marketing',
    subcategories: [
      {
        id: 'social-media',
        label: 'Social Media',
        role: 'social media marketing',
        focus: [
          'Plan a posting calendar that suits each platform.',
          'Draft posts in the brand\'s voice, with visuals where they help.',
          'Publish nothing until the human approves it, and report what performed.',
        ],
      },
      {
        id: 'youtube',
        label: 'YouTube',
        role: 'YouTube marketing',
        focus: [
          'Research topics and keywords the audience searches for.',
          'Draft titles, descriptions, scripts and thumbnail ideas.',
          'Track views, watch time and subscribers, and suggest what to try next.',
        ],
      },
      {
        id: 'content-seo',
        label: 'Content & SEO',
        role: 'content marketing and SEO',
        focus: [
          'Plan articles around what the audience searches for.',
          'Write clear, accurate drafts with sources for every claim.',
          'Track rankings and traffic, and refresh pages that slip.',
        ],
      },
      {
        id: 'email',
        label: 'Email',
        role: 'email marketing',
        focus: [
          'Plan campaigns and newsletters for each audience segment.',
          'Draft subject lines and bodies, and suggest what to A/B test.',
          'Send nothing until the human approves it, and report opens and clicks.',
        ],
      },
    ],
  },
  {
    id: 'research',
    label: 'Research',
    subcategories: [
      {
        id: 'researcher',
        label: 'Researcher',
        role: 'research',
        focus: [
          'Gather sources, and cite one for every claim.',
          'Separate what is known from what is assumed, and say how sure you are.',
          'End with a short summary and the open questions.',
        ],
      },
      {
        id: 'finance-analyst',
        label: 'Finance Analyst',
        role: 'financial analysis',
        focus: [
          'Build models and reports from data the human can check, and show the working.',
          'State every assumption, and how the result changes when it does.',
          'This is analysis, not advice: never move money or trade.',
        ],
      },
      {
        id: 'data-analyst',
        label: 'Data Analyst',
        role: 'data analysis',
        focus: [
          'Check the data for gaps and errors before drawing conclusions.',
          'Answer with a chart or table and one plain sentence on what it shows.',
          'Keep queries and scripts so every number can be reproduced.',
        ],
      },
      {
        id: 'market-research',
        label: 'Market Research',
        role: 'market research',
        focus: [
          'Size the market and map the competitors, with sources.',
          'Summarise what customers say they need, in their own words.',
          'End with the opportunities and risks you found.',
        ],
      },
    ],
  },
  {
    id: 'legal',
    label: 'Legal',
    subcategories: [
      {
        id: 'contracts',
        label: 'Contract Review',
        role: 'reviewing and drafting contracts',
        focus: [
          'Summarise each contract\'s obligations, deadlines, fees and termination terms.',
          'Flag unusual or one-sided clauses, and suggest wording.',
          'This is not legal advice: a qualified lawyer reviews before anything is signed or sent.',
        ],
      },
      {
        id: 'legal-research',
        label: 'Legal Research',
        role: 'legal research',
        focus: [
          'Find the laws, regulations and cases that apply, and cite each one.',
          'Say which jurisdiction and date every answer holds for.',
          'This is not legal advice: a qualified lawyer reviews the conclusions.',
        ],
      },
      {
        id: 'compliance',
        label: 'Compliance',
        role: 'compliance',
        focus: [
          'Map the rules that apply (privacy, consumer, industry) to what the business does.',
          'Keep a checklist of gaps, with owners and deadlines.',
          'This is not legal advice: a qualified lawyer signs off on it.',
        ],
      },
    ],
  },
  {
    id: 'hr',
    label: 'HR',
    subcategories: [
      {
        id: 'recruiter',
        label: 'Recruiter',
        role: 'recruiting',
        focus: [
          'Write job descriptions that say plainly what the role is and needs.',
          'Source and screen candidates against the role\'s criteria, and note why each fits.',
          'Contact no candidate and make no decision without the human.',
        ],
      },
      {
        id: 'onboarding',
        label: 'Onboarding',
        role: 'onboarding new hires',
        focus: [
          'Prepare checklists, accounts and first-week plans ahead of each start date.',
          'Keep onboarding documents current, with one owner each.',
          'Collect feedback from new hires, and suggest what to change.',
        ],
      },
      {
        id: 'people-ops',
        label: 'People Ops',
        role: 'people operations',
        focus: [
          'Keep policies, handbooks and records organised and up to date.',
          'Track leave, reviews and renewals, and remind people ahead of deadlines.',
          'Treat personal data as confidential, and share it with no one without the human.',
        ],
      },
    ],
  },
  {
    id: 'ops',
    label: 'Ops',
    subcategories: [
      {
        id: 'devops',
        label: 'DevOps',
        role: 'infrastructure and DevOps',
        focus: [
          'Keep deployments repeatable, with every change in version control.',
          'Watch for alerts, cost and security issues, and report them.',
          'Ask before anything that could cause downtime or lose data.',
        ],
      },
      {
        id: 'support',
        label: 'Support',
        role: 'customer support',
        focus: [
          'Triage incoming requests by urgency and topic.',
          'Draft friendly, accurate replies, and escalate what needs the human.',
          'Turn repeated questions into help articles.',
        ],
      },
      {
        id: 'admin',
        label: 'Admin & Finance',
        role: 'business operations',
        focus: [
          'Keep documents, invoices and schedules organised.',
          'Prepare reports and reminders ahead of deadlines.',
          'Never make a payment or sign anything without the human.',
        ],
      },
    ],
  },
];

function missionFor(category, sub) {
  if (sub.mission) return sub.mission;
  const focus = sub.focus.map((line) => `- ${line}`).join('\n');
  return `This workspace is for ${sub.role} (${category.label}).

**Focus**
${focus}

${WORKSPACE_WORKING_RULES}${category.closing ? `\n\n${category.closing}` : ''}`;
}

/**
 * The categories offered above a mission field, each with its specialities
 * and the mission text choosing one pre-fills. All of them are shown at once,
 * one row per category, so a mission is always a single click away.
 */
export const MISSION_CATEGORIES = CATALOGUE.map((category) => ({
  id: category.id,
  label: category.label,
  subcategories: category.subcategories.map((sub) => ({
    id: sub.id,
    label: sub.label,
    mission: missionFor(category, sub),
  })),
}));

const ALL_MISSIONS = MISSION_CATEGORIES.flatMap((c) => c.subcategories.map((s) => s.mission));

/**
 * Whether a mission can be replaced by a template without losing anything:
 * it is empty, the form's default, or a template nobody has edited since.
 */
export function isUntouchedMission(text) {
  const trimmed = (text ?? '').trim();
  return trimmed === '' || trimmed === DEFAULT_WORKSPACE_MISSION || ALL_MISSIONS.includes(trimmed);
}

/** The category and speciality whose template the text is, unedited, or null. */
export function findMissionTemplate(text) {
  const trimmed = (text ?? '').trim();
  for (const category of MISSION_CATEGORIES) {
    const sub = category.subcategories.find((s) => s.mission === trimmed);
    if (sub) return { categoryId: category.id, subcategoryId: sub.id };
  }
  return null;
}
