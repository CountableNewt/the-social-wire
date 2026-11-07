import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
const root=join(import.meta.dir,'../../..');
const read=(path:string)=>readFileSync(join(root,path),'utf8');
const spec=Bun.YAML.parse(read('packages/spec/openapi.yaml')) as any;
test('listener state is private revision CAS and supports exact rewind source positions',()=>{
 expect(spec.paths['/xrpc/app.thesocialwire.podcast.putState'].post.responses['409']).toBeDefined();
 expect(spec.components.schemas.PodcastListenerState.properties.playbackSpeed).toEqual({type:'number',minimum:.75,maximum:2,multipleOf:.25});
 expect(spec.components.schemas.PodcastStateSnapshot.required).toEqual(['revision','state']);
 expect(read('database/migrations/20261005010000_podcast_listener.sql')).toContain('podcast_viewer_state');
 expect(read('packages/go/podcastcore/state_store.go')).toContain('revision=');
});
test('podcast media and private/public clips preserve HTTP ranges and server contracts',()=>{
 expect(Object.keys(spec.paths).some(path=>path.startsWith('/v1/podcasts'))).toBe(false);
 for(const path of ['/xrpc/app.thesocialwire.podcast.getMedia','/xrpc/app.thesocialwire.podcast.getClipAsset','/xrpc/app.thesocialwire.podcast.getPublicClipAsset']) {
  expect(spec.paths[path].get.responses['206']).toBeDefined();expect(spec.paths[path].get.responses['416']).toBeDefined();
 }
 expect(spec.paths['/xrpc/app.thesocialwire.podcast.publishClip'].post).toBeDefined();expect(spec.paths['/xrpc/app.thesocialwire.podcast.deleteClip'].post).toBeDefined();
 expect(spec.components.schemas.PodcastTranscript.properties.cues.items.properties.startSeconds.type).toBe('number');
 for(const service of ['appview','gateway']) expect(read(`services/${service}/bruno/XRPC/app.thesocialwire.podcast.publishClip.bru`)).toContain('/xrpc/app.thesocialwire.podcast.publishClip');
});
test('podcasts remain gated and isolated from article retention and public private history',()=>{
 expect(read('services/appview/internal/runtime/host.go')).toContain('if c.PodcastsEnabled');
 expect(read('services/appview/internal/config/config.go')).toContain('strings.EqualFold(env["PODCASTS_ENABLED"], "true")');
 const migration=read('database/migrations/20261005010000_podcast_listener.sql');
 expect(migration).not.toContain('expires_at');expect(migration).toContain('dedupe_key text UNIQUE NOT NULL');
 expect(read('packages/go/podcastcore/protocol.go')).toContain('org.atpodcasting.podcast');
 expect(read('packages/go/podcastcore/protocol.go')).toContain('place.pod.show');
 expect(read('packages/go/podcastcore/protocol.go')).toContain('live.voxport.podcast.series');
});

test('private RSS subscriptions are viewer-owned and excluded from public processing',()=>{
 for(const [path,method] of [['/xrpc/app.thesocialwire.podcast.resolvePrivateShow','post'],['/xrpc/app.thesocialwire.podcast.refreshPrivateShow','post'],['/xrpc/app.thesocialwire.podcast.deletePrivateSubscription','post']]) {
  expect(spec.paths[path][method].security).toEqual([{ATProtoOAuthDPoP:[]}]);
 }
 for(const service of ['appview','gateway']) for(const name of ['resolvePrivateShow','refreshPrivateShow','deletePrivateSubscription']) expect(read(`services/${service}/bruno/XRPC/app.thesocialwire.podcast.${name}.bru`)).toContain(`/xrpc/app.thesocialwire.podcast.${name}`);
 const privateSchema=read('database/migrations/20261006010000_private_podcast_subscriptions.sql');
 expect(privateSchema).toContain('PRIMARY KEY(viewer_did,id)');
 expect(privateSchema).toContain('feed_data text NOT NULL');
 expect(privateSchema).toContain('episode_data text NOT NULL');
 expect(privateSchema).not.toContain('feed_url');
 expect(privateSchema).not.toContain('podcast_jobs');
 expect(privateSchema).not.toContain('podcast_aliases');
 expect(spec.components.schemas.PodcastShow.properties.visibility.enum).toEqual(['private']);
 expect(spec.components.schemas.PodcastEpisode.properties.visibility.enum).toEqual(['private']);
});

test('publisher chapter and host metadata use shared arrays and private artwork proxy',()=>{
 expect(spec.components.schemas.PodcastEpisode.properties.chapters.items.$ref).toBe('#/components/schemas/PodcastChapter');
 expect(spec.components.schemas.PodcastShow.properties.hosts.items.$ref).toBe('#/components/schemas/PodcastPerson');
 expect(spec.paths['/xrpc/app.thesocialwire.podcast.getImage'].get.security).toEqual([{ATProtoOAuthDPoP:[]}]);
 for(const service of ['appview','gateway']) expect(read(`services/${service}/bruno/XRPC/app.thesocialwire.podcast.getImage.bru`)).toContain('/xrpc/app.thesocialwire.podcast.getImage');
 expect(read('services/appview/internal/podcasts/job_routes.go')).toContain('silence:v2:');
 expect(read('services/podcast-worker/.env.example')).toContain('PODCAST_BRIDGE_ENABLED=false');
});

test('library search protects viewer queries and uses bounded continuation pages',()=>{
 const search=spec.paths['/xrpc/app.thesocialwire.podcast.findPodcasts'].post;
 expect(search.security).toEqual([{ATProtoOAuthDPoP:[]}]);
 expect(spec.paths['/xrpc/app.thesocialwire.podcast.findPodcasts'].get).toBeUndefined();
 expect(search.requestBody.content['application/json'].schema.$ref).toBe('#/components/schemas/PodcastSearchRequest');
 expect(spec.components.schemas.PodcastSearchRequest.properties.scope.enum).toEqual(['library','directory']);
 expect(spec.components.schemas.PodcastSearchResponse.required).toEqual(['shows','episodes','hasMore']);
 for(const service of ['appview','gateway']) expect(read(`services/${service}/bruno/XRPC/app.thesocialwire.podcast.findPodcasts.bru`)).toContain('/xrpc/app.thesocialwire.podcast.findPodcasts');
 const store=read('packages/go/podcastcore/search_store.go');
 expect(store).toContain('LIMIT 501');expect(store).toContain('scanned >= 500');
 expect(store).not.toContain('INSERT');expect(store).not.toContain('UPDATE');
});

test('Podcast Index discovery is explicit and returns bounded public candidates',()=>{
 expect(spec.components.schemas.PodcastDirectoryCandidate.required).toEqual(['provider','id','title','feedUrl']);
 expect(spec.components.schemas.PodcastDirectoryCandidate.properties.provider.enum).toEqual(['podcastindex']);
 expect(spec.components.schemas.PodcastSearchResponse.properties.directoryLimit.enum).toEqual([50]);
 expect(spec.paths['/xrpc/app.thesocialwire.podcast.findPodcasts'].post.responses['502']).toBeDefined();
 for(const service of ['appview','gateway']) expect(read(`services/${service}/bruno/XRPC/app.thesocialwire.podcast.findPodcasts-directory.bru`)).toContain('"scope": "directory"');
 const source=read('services/appview/internal/podcasts/directory.go');
 expect(source).toContain('https://api.podcastindex.org/search');expect(source).toContain('User-Agent');
 expect(source).toContain('ValidateURL: func(raw string) bool { return raw == target }');expect(source).toContain('Timeout: 8 * time.Second');
 expect(source).not.toContain('Authorization');
});
