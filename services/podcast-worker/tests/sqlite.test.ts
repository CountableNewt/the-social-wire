import { afterEach, beforeEach, describe, expect, it } from "bun:test";
import { S3Client } from "bun";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import { SQLitePodcastDatabase } from "../src/sqlite";
import { PodcastWorker } from "../src/worker";
import { upsertPodcastEpisode } from "../src/catalog";
import { mediaRuntimeConfiguration } from "../src/runtime";
import type { PodcastEpisode, PodcastJob } from "../src/types";

// Execute the schema owned by Swift so test fixtures cannot silently drift from
// the database shared by AppView and the in-container Bun processor.
const source=await Bun.file(new URL("../../../packages/swift/thin-app-view-core/Sources/ThinAppViewCore/Podcasts/SQLitePodcastStore+Schema.swift",import.meta.url)).text();
const schema=source.match(/sql:\s*"""([\s\S]*?)"""/)?.[1];
if(!schema)throw new Error("Swift podcast schema fixture is missing");
let directory:string;let database:SQLitePodcastDatabase;let second:SQLitePodcastDatabase;
const now=()=>Date.now()/1000;
const episode:PodcastEpisode={id:"episode",showId:"show",title:"Episode",guid:"original-guid",publishedAt:new Date(0).toISOString(),audioUrl:"https://example.com/audio.mp3",transcripts:[]};
function seedJob(kind="transcript",payload:Record<string,unknown>={},attempts=0) {
  const id=randomUUID();
  database.sqlite.query("INSERT INTO podcast_jobs(id,kind,dedupe_key,payload_json,attempts) VALUES(?,?,?,?,?)").run(id,kind,id,JSON.stringify(payload),attempts);
  return id;
}
function seedClip() {
  const id=randomUUID();
  database.sqlite.query("INSERT INTO podcast_clips(id,viewer_did,episode_id,clip_json) VALUES(?,'did:plc:viewer','episode',?)").run(id,JSON.stringify({id,status:"queued",title:"Keep Me",nested:{retained:true}}));
  return id;
}

describe("podcast processor on the shared Swift SQLite schema",()=>{
  beforeEach(()=>{
    directory=mkdtempSync(join(tmpdir(),"podcast-worker-sqlite-"));
    database=new SQLitePodcastDatabase(join(directory,"reader.sqlite"));
    database.sqlite.exec(schema!);
    database.sqlite.query("INSERT INTO podcast_shows(id,source_kind,show_json) VALUES('show','rss',?)").run(JSON.stringify({id:"show",title:"Show",sourceKind:"rss"}));
    database.sqlite.query("INSERT INTO podcast_episodes(id,show_id,guid,episode_json,published_at) VALUES('episode','show','original-guid',?,0)").run(JSON.stringify(episode));
    second=new SQLitePodcastDatabase(join(directory,"reader.sqlite"));
  });
  afterEach(()=>{second.close();database.close();rmSync(directory,{recursive:true,force:true});});

  it("uses one atomic lease across connections and fences heartbeat, completion and failure",()=>{
    const clip=seedClip();const id=seedJob("clip",{clipId:clip});const instant=now();
    const first=database.claim("one",false,instant)!;
    expect(first.id).toBe(id);expect(first.attempts).toBe(1);
    expect(second.claim("two",false,instant)).toBeUndefined();
    expect(database.heartbeat(id,"one",instant+121)).toBe(false);
    const reclaimed=second.claim("two",false,instant+121)!;
    expect(reclaimed.attempts).toBe(2);
    expect(database.complete(first,"one",{audioKey:"stale"},instant+122)).toBe(false);
    expect(database.fail({...first,attempts:3},"one","stale failure",instant+122)).toBe(false);
    expect(database.clip(clip)?.clip_json.status).toBe("queued");
    expect(second.complete(reclaimed,"two",{audioKey:"new",nullable:null},instant+122)).toBe(true);
    expect(database.clip(clip)?.clip_json).toMatchObject({status:"complete",title:"Keep Me",nested:{retained:true},audioKey:"new",nullable:null});
    expect(database.heartbeat(id,"two",instant+123)).toBe(false);
  });

  it("claims exactly once when separate Bun processes compete",async()=>{
    const id=seedJob();
    const modulePath=new URL("../src/sqlite.ts",import.meta.url).pathname;
    const script=`import {SQLitePodcastDatabase} from ${JSON.stringify(modulePath)};const db=new SQLitePodcastDatabase(process.argv[1]);const job=db.claim(process.argv[2],false);console.log(JSON.stringify(job?.id??null));db.close();`;
    const children=["first","second"].map(worker=>Bun.spawn([process.execPath,"-e",script,join(directory,"reader.sqlite"),worker],{stdout:"pipe",stderr:"pipe"}));
    const output=await Promise.all(children.map(async child=>{
      const value=await new Response(child.stdout).text();
      expect(await child.exited).toBe(0);
      return JSON.parse(value);
    }));
    expect(output.filter(Boolean)).toEqual([id]);
  });

  it("bounds expired attempts, fails the clip once, and permits an explicit queued retry",()=>{
    const clip=seedClip();const id=seedJob("clip",{clipId:clip},2);const instant=now();
    const job=database.claim("one",false,instant)!;
    expect(job.attempts).toBe(3);
    expect(second.claim("two",false,instant+121)).toBeUndefined();
    expect(database.clip(clip)?.clip_json).toMatchObject({status:"failed",error:"Processing lease expired",title:"Keep Me"});
    database.sqlite.query("UPDATE podcast_jobs SET status='queued' WHERE id=?").run(id);
    expect(second.claim("two",false,instant+122)?.attempts).toBe(4);
  });

  it("deduplicates bridge jobs and never claims them with bridging disabled",()=>{
    database.enqueueBridge("one",{showId:"show"});database.enqueueBridge("one",{showId:"show"});
    expect(database.claim("worker",false)).toBeUndefined();
    expect(database.claim("worker",true)?.kind).toBe("bridge");
    expect(database.claim("worker",true)).toBeUndefined();
  });

  it("keeps canonical protocol identity and chapter enrichment through RSS refresh",async()=>{
    const enriched={...episode,sourceUri:"at://did:plc:publisher/place.pod.episode/one",durationSeconds:42,transcripts:[{url:"https://example.com/captions.vtt",type:"text/vtt"}],chapters:[{startSeconds:0,title:"Intro"}]};
    database.sqlite.query("UPDATE podcast_episodes SET episode_json=? WHERE id='episode'").run(JSON.stringify(enriched));
    await upsertPodcastEpisode(database,{...episode,id:"rss-derived-id",title:"Updated",audioUrl:"https://example.com/new.mp3"});
    expect(database.episodes("show")).toEqual([{...enriched,title:"Updated",audioUrl:"https://example.com/new.mp3"}]);
    database.upsertProtocolEpisode({...episode,id:"at://did:plc:publisher/place.pod.episode/one",sourceUri:enriched.sourceUri},enriched.sourceUri);
    expect(database.episodes("show")[0]?.id).toBe("episode");
    expect(database.episodes("show")[0]?.chapters).toEqual(enriched.chapters);
    expect(database.hasAlias(enriched.sourceUri)).toBe(true);
    database.upsertProtocolEpisode({...episode,id:enriched.sourceUri,chapterSourceUrl:"https://example.com/new-chapters.json"},enriched.sourceUri);
    expect(database.episodes("show")[0]?.chapters).toEqual([]);
  });

  it("matches JSONB shallow merge semantics without dropping nulls or existing fields",()=>{
    database.mergeEpisode("episode",{sourceUri:null,durationSeconds:12});
    expect(database.episodes("show")[0]).toMatchObject({title:"Episode",sourceUri:null,durationSeconds:12});
    database.mergeShow("show",{sourceUri:"at://did:plc:bridge/org.atpodcasting.podcast/show",guid:"guid"});
    database.addAlias("guid:guid","show","show");
    expect(database.show("show")).toMatchObject({title:"Show",guid:"guid"});
    expect(database.hasAlias("guid:guid")).toBe(true);
    expect(database.subscribed("show")).toBe(false);
    database.sqlite.query("INSERT INTO podcast_subscriptions(viewer_did,show_id,source_uri) VALUES('did:plc:one','show','at://subscription')").run();
    expect(database.subscribed("show")).toBe(true);
    expect(database.subscribedShows()).toHaveLength(1);
  });

  it("runs worker completion and retry paths against SQLite without losing clip metadata",async()=>{
    const clip=seedClip();seedJob("clip",{clipId:clip});
    class SuccessfulWorker extends PodcastWorker {override async process(){return {durationSeconds:12};}}
    await new SuccessfulWorker(database,new S3Client()).tick();
    expect(database.clip(clip)?.clip_json).toMatchObject({status:"complete",durationSeconds:12,title:"Keep Me"});
    const retry=seedJob();
    class FailingWorker extends PodcastWorker {override async process():Promise<Record<string,unknown>>{throw new Error("Download failed https://example.com/private?token=secret");}}
    await new FailingWorker(database,new S3Client()).tick();
    const row=database.sqlite.query<{status:string;error:string;available_at:number},[string]>("SELECT status,error,available_at FROM podcast_jobs WHERE id=?").get(retry)!;
    expect(row.status).toBe("queued");expect(row.error).toBe("Download failed [publisher]");expect(row.available_at).toBeGreaterThan(now());
  });

  it("does not fail a clip after the running worker loses ownership",async()=>{
    const clip=seedClip();const id=seedJob("clip",{clipId:clip},2);
    class StaleWorker extends PodcastWorker {
      override async process(_job:PodcastJob):Promise<Record<string,unknown>> {
        second.sqlite.query("UPDATE podcast_jobs SET worker_id='replacement',status='complete' WHERE id=?").run(id);
        second.sqlite.query("UPDATE podcast_clips SET clip_json=? WHERE id=?").run(JSON.stringify({status:"complete",audioKey:"replacement"}),clip);
        throw new Error("Old worker failed");
      }
    }
    await new StaleWorker(database,new S3Client()).tick();
    expect(database.clip(clip)?.clip_json).toEqual({status:"complete",audioKey:"replacement"});
  });

  it("rejects cleanup of a published clip before contacting object storage",async()=>{
    const clip=seedClip();database.sqlite.query("UPDATE podcast_clips SET published_uri='at://did:plc:one/app.thesocialwire.podcast.clip/published' WHERE id=?").run(clip);
    await expect(new PodcastWorker(database,new S3Client()).process({id:randomUUID(),kind:"cleanup",viewer_did:null,episode_id:null,payload_json:{clipId:clip},attempts:1})).rejects.toThrow("Published clips cannot be cleaned up");
  });
});

describe("in-container podcast processor configuration",()=>{
  it("uses the shared volume and a private independent port",()=>{
    expect(mediaRuntimeConfiguration({BACKEND_MODE:"standalone",SQLITE_DB_PATH:"/data/reader.sqlite",PORT:"8081"})).toEqual({standalone:true,hostname:"127.0.0.1",port:9080,sqlitePath:"/data/reader.sqlite"});
    expect(()=>mediaRuntimeConfiguration({BACKEND_MODE:"standalone",SQLITE_DB_PATH:"/data/reader.sqlite",PODCAST_BIND_HOST:"0.0.0.0"})).toThrow("127.0.0.1");
    expect(()=>mediaRuntimeConfiguration({BACKEND_MODE:"standalone",SQLITE_DB_PATH:"relative.sqlite"})).toThrow("absolute");
    expect(()=>mediaRuntimeConfiguration({PODCAST_MEDIA_PORT:"-1"})).toThrow("port");
  });
});
