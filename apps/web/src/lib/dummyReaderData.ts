import type { EntryDetail, EntryListItem } from "@/lib/atprotoClient";
import type {
  PublicationSidebarProjection,
  SidebarPublicationRow,
} from "@/lib/publicationProjectionClient";
import type { ViewerProfileSlice } from "@/hooks/useViewerProfile";

export const DUMMY_VIEWER_DID = "did:plc:socialwire-dummy-viewer";

export function isDummyReaderDataEnabled(): boolean {
  return process.env.NEXT_PUBLIC_USE_DUMMY_DATA === "true";
}

const now = "2026-07-06T12:00:00.000Z";

const folderSeeds = [
  { rkey: "industry", name: "Industry", icon: "briefcase" },
  { rkey: "product", name: "Product", icon: "palette" },
  { rkey: "engineering", name: "Engineering", icon: "code" },
  { rkey: "policy", name: "Policy", icon: "scale" },
] as const;

const explicitPublicationIds = {
  standard:
    "at://did:plc:standard-dummy/site.standard.publication/the-standard",
  wire:
    "at://did:plc:wire-dummy/site.standard.publication/social-wire-digest",
  design:
    "at://did:plc:design-dummy/site.standard.publication/interface-notes",
  policy:
    "at://did:plc:policy-dummy/site.standard.publication/civic-signal",
};

type PublicationSeed = {
  key: string;
  publicationId: string;
  authorDid: string;
  authorHandle: string;
  title: string;
  tab: "subscribed" | "following";
  folderRkey?: (typeof folderSeeds)[number]["rkey"];
  unreadCount: number;
  topics: string[];
};

function publicationAtUri(key: string): string {
  return `at://did:plc:${key}-dummy/site.standard.publication/${key}`;
}

const publicationSeeds: PublicationSeed[] = [
  {
    key: "standard",
    publicationId: explicitPublicationIds.standard,
    authorDid: "did:plc:standard-dummy",
    authorHandle: "standard.example",
    title: "The Standard",
    tab: "subscribed",
    folderRkey: "industry",
    unreadCount: 8,
    topics: ["launch planning", "distribution", "reader operations"],
  },
  {
    key: "newsroom-index",
    publicationId: publicationAtUri("newsroom-index"),
    authorDid: "did:plc:newsroom-index-dummy",
    authorHandle: "newsroom.example",
    title: "Newsroom Index",
    tab: "subscribed",
    folderRkey: "industry",
    unreadCount: 5,
    topics: ["assignment desks", "editorial calendars", "audience signals"],
  },
  {
    key: "product-weekly",
    publicationId: publicationAtUri("product-weekly"),
    authorDid: "did:plc:product-weekly-dummy",
    authorHandle: "product.example",
    title: "Product Weekly",
    tab: "subscribed",
    folderRkey: "product",
    unreadCount: 6,
    topics: ["roadmaps", "reader workflows", "release quality"],
  },
  {
    key: "interface-notes",
    publicationId: explicitPublicationIds.design,
    authorDid: "did:plc:design-dummy",
    authorHandle: "interface.example",
    title: "Interface Notes",
    tab: "subscribed",
    folderRkey: "product",
    unreadCount: 4,
    topics: ["toolbars", "list density", "accessibility"],
  },
  {
    key: "ops-ledger",
    publicationId: publicationAtUri("ops-ledger"),
    authorDid: "did:plc:ops-ledger-dummy",
    authorHandle: "ops.example",
    title: "Ops Ledger",
    tab: "subscribed",
    folderRkey: "engineering",
    unreadCount: 7,
    topics: ["gateway reliability", "cache health", "deploy checks"],
  },
  {
    key: "infra-review",
    publicationId: publicationAtUri("infra-review"),
    authorDid: "did:plc:infra-review-dummy",
    authorHandle: "infra.example",
    title: "Infra Review",
    tab: "subscribed",
    folderRkey: "engineering",
    unreadCount: 2,
    topics: ["queues", "database indexes", "observability"],
  },
  {
    key: "civic-signal",
    publicationId: explicitPublicationIds.policy,
    authorDid: "did:plc:policy-dummy",
    authorHandle: "civic.example",
    title: "Civic Signal",
    tab: "subscribed",
    folderRkey: "policy",
    unreadCount: 5,
    topics: ["rulemaking", "public comments", "platform policy"],
  },
  {
    key: "research-desk",
    publicationId: publicationAtUri("research-desk"),
    authorDid: "did:plc:research-desk-dummy",
    authorHandle: "research.example",
    title: "Research Desk",
    tab: "subscribed",
    unreadCount: 3,
    topics: ["media research", "survey data", "market structure"],
  },
  {
    key: "social-wire-digest",
    publicationId: explicitPublicationIds.wire,
    authorDid: "did:plc:wire-dummy",
    authorHandle: "digest.thesocialwire.local",
    title: "Social Wire Digest",
    tab: "subscribed",
    unreadCount: 6,
    topics: ["reader cache", "bulk read state", "mobile panes"],
  },
  {
    key: "standards-watch",
    publicationId: publicationAtUri("standards-watch"),
    authorDid: "did:plc:standards-watch-dummy",
    authorHandle: "standards.example",
    title: "Standards Watch",
    tab: "following",
    unreadCount: 4,
    topics: ["ATProto", "standard.site", "portable identity"],
  },
  {
    key: "design-systems",
    publicationId: publicationAtUri("design-systems"),
    authorDid: "did:plc:design-systems-dummy",
    authorHandle: "systems.example",
    title: "Design Systems Daily",
    tab: "following",
    unreadCount: 3,
    topics: ["component APIs", "tokens", "interaction states"],
  },
  {
    key: "local-lab",
    publicationId: publicationAtUri("local-lab"),
    authorDid: "did:plc:local-lab-dummy",
    authorHandle: "local.example",
    title: "Local Lab",
    tab: "following",
    unreadCount: 1,
    topics: ["community news", "events", "membership"],
  },
  {
    key: "rss-notebook",
    publicationId:
      "rss:https%3A%2F%2Fnotebook.example%2Ffeeds%2Fupdates.xml",
    authorDid: "did:web:skyreader.rss",
    authorHandle: "notebook.example",
    title: "RSS Notebook",
    tab: "following",
    unreadCount: 2,
    topics: ["feed parsing", "syndication", "metadata"],
  },
];

function scope(authorDid: string, publicationAtUri: string) {
  return {
    authorDid,
    publicationAtUri: publicationAtUri.startsWith("at://")
      ? publicationAtUri
      : null,
    publicationScopeAtUris: publicationAtUri.startsWith("at://")
      ? [publicationAtUri]
      : [],
    publicationSiteUrls: [],
  };
}

function rowFromSeed(seed: PublicationSeed): SidebarPublicationRow {
  return {
    publicationId: seed.publicationId,
    subscriptionPublicationId: seed.publicationId,
    authorDid: seed.authorDid,
    authorHandle: seed.authorHandle,
    title: seed.title,
    discoveredAt: now,
    appViewScope: scope(seed.authorDid, seed.publicationId),
    unreadCount: seed.unreadCount,
  };
}

const allRows = publicationSeeds.map(rowFromSeed);
const rowsById = new Map(allRows.map((row) => [row.publicationId, row]));

export const dummyPublicationSidebarProjection: PublicationSidebarProjection = {
  viewerDid: DUMMY_VIEWER_DID,
  folders: folderSeeds.map((folder, index) => ({
    uri: `at://${DUMMY_VIEWER_DID}/app.thesocialwire.folder/${folder.rkey}`,
    rkey: folder.rkey,
    value: {
      name: folder.name,
      icon: folder.icon,
      sortOrder: index,
      createdAt: now,
    },
  })),
  publicationPrefs: publicationSeeds
    .filter((seed) => seed.tab === "subscribed")
    .map((seed, index) => ({
      uri: `at://${DUMMY_VIEWER_DID}/app.thesocialwire.publicationPrefs/${seed.key}`,
      publicationId: seed.publicationId,
      value: {
        publicationId: seed.publicationId,
        folderId: seed.folderRkey,
        sortOrder: index,
        createdAt: now,
      },
    })),
  folderSections: folderSeeds.map((folder) => ({
    folderRkey: folder.rkey,
    folderUri: `at://${DUMMY_VIEWER_DID}/app.thesocialwire.folder/${folder.rkey}`,
    publications: publicationSeeds
      .filter((seed) => seed.folderRkey === folder.rkey)
      .map((seed) => rowsById.get(seed.publicationId)!)
      .filter(Boolean),
  })),
  allPublicationRows: allRows,
  myPublications: [],
  subscribedUnfoldered: publicationSeeds
    .filter((seed) => seed.tab === "subscribed" && !seed.folderRkey)
    .map((seed) => rowsById.get(seed.publicationId)!)
    .filter(Boolean),
  followingTabPublications: publicationSeeds
    .filter((seed) => seed.tab === "following")
    .map((seed) => rowsById.get(seed.publicationId)!)
    .filter(Boolean),
  enrollAuthorDids: publicationSeeds.map((seed) => seed.authorDid),
  refreshedAt: now,
  unreadCountsByPublicationId: Object.fromEntries(
    publicationSeeds.map((seed) => [seed.publicationId, seed.unreadCount])
  ),
};

const entryTemplates = [
  {
    title: "Morning brief: {topic} and the next operational decision",
    summary:
      "A compact briefing on what changed, what is blocked, and what deserves attention before the next review.",
  },
  {
    title: "Field notes from a week of {topic}",
    summary:
      "Practical observations from real reader workflows, with the edge cases called out early.",
  },
  {
    title: "What teams keep missing about {topic}",
    summary:
      "A closer look at the small product and process details that compound over repeated publishing cycles.",
  },
  {
    title: "Checklist: making {topic} easier to scan",
    summary:
      "A tight checklist for turning complex source material into a reader surface that works under time pressure.",
  },
  {
    title: "Signals to watch in {topic}",
    summary:
      "The leading indicators that usually show whether a workflow is healthy, stale, or drifting.",
  },
  {
    title: "How {topic} changes the sidebar conversation",
    summary:
      "Why navigation structure, unread counts, and saved context need to move together.",
  },
  {
    title: "A quieter design pass for {topic}",
    summary:
      "Reducing visible chrome while preserving the state and affordances a frequent reader depends on.",
  },
  {
    title: "Open questions around {topic}",
    summary:
      "Tradeoffs, risks, and follow-up decisions worth annotating before the implementation hardens.",
  },
];

function slug(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

function dummyEntriesForSeed(seed: PublicationSeed): EntryListItem[] {
  if (seed.key === "social-wire-digest") {
    return [
      {
        entryId: `at://${seed.authorDid}/site.standard.document/morning-wire`,
        title: "Morning wire: reader cache, bulk read state, and mobile panes",
        summary:
          "A focused digest of product changes across reader startup, saved links, and compact navigation.",
        publishedAt: "2026-07-06T08:00:00.000Z",
        originalUrl: "https://thesocialwire.local/digest/morning-wire",
      },
      {
        entryId: `at://${seed.authorDid}/site.standard.document/appview-ops`,
        title: "AppView operations notes for the week",
        summary:
          "Gateway checks, projection cache invalidation, and read mark sync risks to keep in view.",
        publishedAt: "2026-07-03T19:10:00.000Z",
        originalUrl: "https://thesocialwire.local/digest/appview-ops",
      },
      ...entryTemplates.slice(2).map((template, index) =>
        entryFromTemplate(seed, template, index + 2)
      ),
    ];
  }

  return entryTemplates.map((template, index) =>
    entryFromTemplate(seed, template, index)
  );
}

function entryFromTemplate(
  seed: PublicationSeed,
  template: (typeof entryTemplates)[number],
  index: number
): EntryListItem {
  const topic = seed.topics[index % seed.topics.length];
  const publishedDate = new Date(Date.UTC(2026, 6, 6 - index, 14 - index, 0, 0));
  const title = template.title.replace("{topic}", topic);
  return {
    entryId: `at://${seed.authorDid}/site.standard.document/${slug(seed.key)}-${index + 1}`,
    title,
    summary: template.summary,
    publishedAt: publishedDate.toISOString(),
    originalUrl: `https://${seed.authorHandle}/articles/${slug(title)}`,
  };
}

const entriesByPublicationId: Record<string, EntryListItem[]> =
  Object.fromEntries(
    publicationSeeds.map((seed) => [
      seed.publicationId,
      dummyEntriesForSeed(seed),
    ])
  );

const entryDetailsById = Object.values(entriesByPublicationId)
  .flat()
  .reduce<Record<string, EntryDetail>>((acc, entry) => {
    acc[entry.entryId] = {
      ...entry,
      contentHtml: `
        <h1>${entry.title}</h1>
        <p>${entry.summary ?? ""}</p>
        <p>This local dummy article gives the reader pane enough body copy for spacing, toolbar, and scrolling annotations without touching live ATProto or Gateway data.</p>
        <p>Use it to mark column proportions, list density, action placement, empty states, and responsive behavior before wiring the next real implementation pass.</p>
        <p>The additional dummy corpus intentionally includes enough rows to exercise sidebar scrolling, section counts, selected states, unread filtering, and longer article lists.</p>
      `,
    };
    return acc;
  }, {});

export function dummyEntriesForPublication(publicationId: string): EntryListItem[] {
  return entriesByPublicationId[publicationId] ?? [];
}

export function dummyEntryDetail(entryId: string): EntryDetail | null {
  return entryDetailsById[entryId] ?? null;
}

export const dummyViewerProfile: ViewerProfileSlice = {
  did: DUMMY_VIEWER_DID,
  handle: "designer.thesocialwire.local",
  displayName: "Design Review",
  description: "Local dummy profile for UI annotation work.",
};
