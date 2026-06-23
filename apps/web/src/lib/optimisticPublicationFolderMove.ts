import { COLLECTION_PUB_PREFS } from "@/lib/pdsClient";
import type {
  PublicationSidebarProjection,
  SidebarPublicationRow,
} from "@/lib/publicationProjectionClient";

export const OPTIMISTIC_PREF_RKEY_PREFIX = "optimistic-pref-";

function findSidebarPublicationRow(
  projection: PublicationSidebarProjection,
  publicationId: string
): SidebarPublicationRow | undefined {
  const unfoldered = projection.subscribedUnfoldered.find(
    (row) => row.publicationId === publicationId
  );
  if (unfoldered) return unfoldered;

  for (const section of projection.folderSections ?? []) {
    const inFolder = section.publications.find(
      (row) => row.publicationId === publicationId
    );
    if (inFolder) return inFolder;
  }

  return projection.allPublicationRows.find(
    (row) => row.publicationId === publicationId
  );
}

function shouldBeInSubscribedUnfoldered(
  projection: PublicationSidebarProjection,
  publicationId: string
): boolean {
  if (
    projection.myPublications.some((row) => row.publicationId === publicationId)
  ) {
    return false;
  }
  if (
    projection.followingTabPublications.some(
      (row) => row.publicationId === publicationId
    )
  ) {
    return false;
  }
  return true;
}

function updatePublicationPrefsForFolderMove(
  publicationPrefs: PublicationSidebarProjection["publicationPrefs"],
  args: {
    viewerDid: string;
    publicationId: string;
    folderId: string | null;
  }
): PublicationSidebarProjection["publicationPrefs"] {
  const index = publicationPrefs.findIndex(
    (pref) => pref.publicationId === args.publicationId
  );

  if (args.folderId === null) {
    if (index < 0) return publicationPrefs;
    const pref = publicationPrefs[index];
    const nextValue = { ...pref.value };
    delete nextValue.folderId;
    return [
      ...publicationPrefs.slice(0, index),
      { ...pref, value: nextValue },
      ...publicationPrefs.slice(index + 1),
    ];
  }

  if (index >= 0) {
    return publicationPrefs.map((pref, prefIndex) =>
      prefIndex === index
        ? {
            ...pref,
            value: {
              ...pref.value,
              folderId: args.folderId,
            },
          }
        : pref
    );
  }

  const optimisticRkey = `${OPTIMISTIC_PREF_RKEY_PREFIX}${crypto.randomUUID()}`;
  return [
    ...publicationPrefs,
    {
      uri: `at://${args.viewerDid}/${COLLECTION_PUB_PREFS}/${optimisticRkey}`,
      publicationId: args.publicationId,
      value: {
        folderId: args.folderId,
        sortOrder: 0,
        createdAt: new Date().toISOString(),
      },
    },
  ];
}

export function applyPublicationFolderMoveToProjection(
  projection: PublicationSidebarProjection | undefined,
  args: {
    publicationId: string;
    folderId: string | null;
  }
): PublicationSidebarProjection | undefined {
  if (!projection) return undefined;

  const row = findSidebarPublicationRow(projection, args.publicationId);
  if (!row) return projection;

  const publicationPrefs = updatePublicationPrefsForFolderMove(
    projection.publicationPrefs,
    {
      viewerDid: projection.viewerDid,
      publicationId: args.publicationId,
      folderId: args.folderId,
    }
  );

  let subscribedUnfoldered = projection.subscribedUnfoldered.filter(
    (entry) => entry.publicationId !== args.publicationId
  );
  if (
    args.folderId === null &&
    shouldBeInSubscribedUnfoldered(projection, args.publicationId)
  ) {
    subscribedUnfoldered = [...subscribedUnfoldered, row];
  }

  let folderSections = projection.folderSections;
  if (folderSections) {
    folderSections = folderSections.map((section) => ({
      ...section,
      publications: section.publications.filter(
        (entry) => entry.publicationId !== args.publicationId
      ),
    }));
    if (args.folderId) {
      folderSections = folderSections.map((section) =>
        section.folderRkey === args.folderId
          ? {
              ...section,
              publications: [...section.publications, row],
            }
          : section
      );
    }
  }

  return {
    ...projection,
    publicationPrefs,
    subscribedUnfoldered,
    folderSections,
  };
}

export function reconcilePublicationPrefAfterWrite(
  projection: PublicationSidebarProjection | undefined,
  publicationId: string,
  written: { uri: string; rkey: string }
): PublicationSidebarProjection | undefined {
  if (!projection) return undefined;

  return {
    ...projection,
    publicationPrefs: projection.publicationPrefs.map((pref) =>
      pref.publicationId === publicationId
        ? {
            ...pref,
            uri: written.uri,
          }
        : pref
    ),
  };
}
