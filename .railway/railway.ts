import { defineRailway, github, preserve, project, service } from "railway/iac";
import { developmentPodcasts } from "./podcasts.ts";

export const partial = "indexing-consolidation";

const repository = "Stygian-Tech/the-social-wire";

const developmentProfile = {
  appEnvironment: "dev",
  branch: "dev",
  workerRegion: "us-west2",
  projectionReplicas: 2,
  appViewGeneration: "jetstream-v2-us-west-finance-sports-v1-20261003",
  projection: {
    POSTGRES_MAX_CONNECTIONS: "8",
    THIN_APPVIEW_INGESTION_INBOX_MAX_CONCURRENCY: "16",
    WIRE_FEED_MODE: "off",
    FINANCE_FEED_MODE: "off",
    SPORTS_FEED_MODE: "off",
    SPORTS_EVENTS_ENABLED: "false",
    WIRE_INBOX_SOURCE_GENERATIONS: "wire-global-v6-dev-publication-finance-live-20261001",
    WIRE_INBOX_CONCURRENCY: "24",
    WIRE_POSTGRES_MAX_CONNECTIONS: "12",
    WIRE_INBOX_CLEANUP_ENABLED: "true",
    WIRE_DEFERRED_RECOMMENDATIONS_ENABLED: "true",
  },
  coordinator: {
    // Enable only after exact parity and representative performance gates pass.
    WIRE_SIGNAL_ROLLUP_INCREMENTAL_ENABLED: "false",
    WIRE_RANK_INTERVAL_SECONDS: "600",
    WIRE_GENERATION_RETENTION_SECONDS: "7200",
    POSTGRES_MAX_CONNECTIONS: "16",
    THIN_APPVIEW_INGESTION_INBOX_MAX_CONCURRENCY: "32",
    WIRE_FEED_MODE: "off",
    FINANCE_FEED_MODE: "off",
    SPORTS_FEED_MODE: "off",
    SPORTS_EVENTS_ENABLED: "false",
    WIRE_INBOX_SOURCE_GENERATIONS: "wire-global-v6-dev-publication-finance-live-20261001",
    WIRE_INBOX_CONCURRENCY: "48",
    WIRE_POSTGRES_MAX_CONNECTIONS: "24",
    WIRE_EXTERNAL_SIGNAL_MODE: "off",
    WIRE_GLOBAL_CANDIDATE_PROJECTION_ENABLED: "true",
    WIRE_METADATA_SCHEDULING_MAINTENANCE_ENABLED: "false",
    WIRE_METADATA_SCHEDULING_READ_ENABLED: "false",
  },
  wireCleanupBatchSize: "20000",
  wireCleanupIdleMilliseconds: "100",
} as const;

const productionWireLiveGenerations = [
  "wire-global-v1-prod-live-v1",
  "wire-global-v3-prod-live-v1",
  "wire-global-v4-prod-live-tail-v1",
  "wire-global-v5-prod-live-tail-v1",
  "wire-global-v6-prod-publication-live-v1",
  "wire-global-v7-prod-publication-live-tail-v1",
  "wire-global-v8-prod-external-live-v1",
  "wire-global-v8-prod-publication-live-tail-v1",
];
const productionWireWestGeneration = "wire-global-v9-prod-publication-west-20260919";
const productionWireSourceGenerations = [
  ...productionWireLiveGenerations, productionWireWestGeneration,
].join(",");

// Retention/recovery covers completed snapshot generations as well as live drains.
const productionWireCoordinatorGenerations = [
  ...productionWireLiveGenerations,
  "wire-global-v8-prod-external-8d-snapshot-v1",
  "wire-global-v8-prod-external-8d-snapshot-a-v1",
  "wire-global-v8-prod-external-8d-snapshot-b-v1",
  productionWireWestGeneration,
].join(",");

const productionProfile = {
  appEnvironment: "prod",
  branch: "main",
  workerRegion: "sfo",
  projectionReplicas: 4,
  appViewGeneration: "jetstream-v2-us-west-v2",
  projection: {
    POSTGRES_MAX_CONNECTIONS: "8",
    THIN_APPVIEW_INGESTION_INBOX_MAX_CONCURRENCY: "8",
    WIRE_FEED_MODE: "off",
    FINANCE_FEED_MODE: "off",
    SPORTS_FEED_MODE: "off",
    SPORTS_EVENTS_ENABLED: "false",
    WIRE_INBOX_CONCURRENCY: "52",
    WIRE_INBOX_SOURCE_GENERATIONS: productionWireSourceGenerations,
    WIRE_POSTGRES_MAX_CONNECTIONS: "12",
    WIRE_INBOX_CLEANUP_ENABLED: "false",
    WIRE_DEFERRED_RECOMMENDATIONS_ENABLED: "true",
    THIN_APPVIEW_MAX_ENROLL_RECORDS_PER_AUTHOR: "3000",
  },
  coordinator: {
    WIRE_SIGNAL_ROLLUP_INCREMENTAL_ENABLED: "false",
    WIRE_RANK_INTERVAL_SECONDS: "600",
    WIRE_GENERATION_RETENTION_SECONDS: "7200",
    POSTGRES_MAX_CONNECTIONS: "16",
    THIN_APPVIEW_INGESTION_INBOX_MAX_CONCURRENCY: "32",
    WIRE_EXTERNAL_SIGNAL_MODE: "off",
    WIRE_FEED_MODE: "off",
    FINANCE_FEED_MODE: "off",
    SPORTS_FEED_MODE: "off",
    SPORTS_EVENTS_ENABLED: "false",
    WIRE_INBOX_CONCURRENCY: "32",
    WIRE_INBOX_SOURCE_GENERATIONS: productionWireCoordinatorGenerations,
    WIRE_METADATA_BATCH_SIZE: "128",
    WIRE_METADATA_CONCURRENCY: "16",
    WIRE_POSTGRES_MAX_CONNECTIONS: "12",
    WIRE_GLOBAL_CANDIDATE_PROJECTION_ENABLED: "true",
    WIRE_METADATA_SCHEDULING_MAINTENANCE_ENABLED: "false",
    WIRE_METADATA_SCHEDULING_READ_ENABLED: "false",
  },
  wireCleanupBatchSize: "5000",
  wireCleanupIdleMilliseconds: "1000",
} as const;

const indexingBuild = {
  builder: "DOCKERFILE" as const,
  dockerfilePath: "/services/indexing-worker/Dockerfile",
  watchPatterns: [
    "/.railway/**",
    "/services/indexing-worker/**",
    "/tools/schema-ready/**",
    "/database/migrations/**",
  ],
};

const longRunningDeploy = {
  healthcheckPath: "/startupz",
  healthcheckTimeout: 120,
  restartPolicyType: "ALWAYS" as const,
};

const retiredWorkerDeploy = {
  ...longRunningDeploy,
  restartPolicyType: "NEVER" as const,
};

export default defineRailway((context) => {
  const profile = context.isEnvironment("dev")
    ? developmentProfile
    : context.isEnvironment("production")
      ? productionProfile
      : undefined;
  if (!profile) {
    throw new Error(
      "The indexing-consolidation partial is authorized only for Development and Production",
    );
  }

  const appViewWorkerEnvironment = {
    APPVIEW_CACHE_BACKEND: "redis",
    APP_ENV: profile.appEnvironment,
    DATABASE_MIGRATOR_SERVICE_ID: preserve(),
    DATABASE_URL: preserve(),
    ENABLE_THIN_APPVIEW: "true",
    JETSTREAM_SOURCE_GENERATION: profile.appViewGeneration,
    OPERATIONS_RECOVERY_ENABLED: "true",
    OPERATIONS_TELEMETRY_ENABLED: "true",
    REDIS_URL: preserve(),
    TAP_CONSUMER_MODE: "disabled",
    THIN_APPVIEW_INGESTION_INBOX_LEASE_SECONDS: "60",
    THIN_APPVIEW_INGESTION_INBOX_POLL_MILLISECONDS: "250",
    THIN_APPVIEW_JETSTREAM_MODE: "v2_authoritative",
    THIN_APPVIEW_PROACTIVE_BACKFILL_ENABLED: "false",
    THIN_APPVIEW_REPOSITORY_RESTORE_TIMEOUT_SECONDS: "120",
    WIRE_ACTOR_HMAC_SECRET: preserve(),
    WIRE_PUBLICATION_CACHE_ENABLED: "true",
    WIRE_DEPENDENCY_HYDRATION_ENABLED: "true",
    WIRE_INBOX_BATCH_SIZE: "5000",
    WIRE_INBOX_CLEANUP_BATCH_SIZE: profile.wireCleanupBatchSize,
    WIRE_INBOX_CLEANUP_ENABLED: "true",
    WIRE_INBOX_CLEANUP_IDLE_MILLISECONDS: profile.wireCleanupIdleMilliseconds,
    WIRE_INBOX_IDLE_MILLISECONDS: "250",
  } as const;

  const projectionPool = service("Projection Pool", {
    source: github(repository, { branch: profile.branch, rootDirectory: "/" }),
    // This service is stopped during the provider cutover, not deleted by omission.
    build: { ...indexingBuild, watchPatterns: ["/__global_projection_retired__/**"] },
    deploy: retiredWorkerDeploy,
    replicas: { [profile.workerRegion]: profile.projectionReplicas },
    env: {
      ...appViewWorkerEnvironment,
      ...profile.projection,
      INDEXING_WORKER_ROLE: "projection",
    },
  });

  const coordinator = service("Coordinator", {
    source: github(repository, { branch: profile.branch, rootDirectory: "/" }),
    build: indexingBuild,
    deploy: longRunningDeploy,
    replicas: { [profile.workerRegion]: 2 },
    env: {
      ...appViewWorkerEnvironment,
      ...profile.coordinator,
      INDEXING_WORKER_ROLE: "coordinator",
      THIN_APPVIEW_WORKER_MODE: "maintenance",
      THIN_APPVIEW_RSS_FEED_POLL_ENABLED: "true",
      OPERATIONS_RECOVERY_ENABLED: "false",
    },
  });

  return project("The Social Wire", {
    resources: [
      projectionPool, coordinator,
      ...(context.isEnvironment("dev") ? developmentPodcasts() : []),
    ],
  });
});
