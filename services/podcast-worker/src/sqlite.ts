import { Database } from "bun:sqlite";
import { randomUUID } from "node:crypto";
import type { SQL } from "bun";
import type { PodcastEpisode, PodcastJob, PodcastShow } from "./types";

type StoredJob = Omit<PodcastJob,"payload_json"> & {payload_json:string};
type ClipRow = {published_uri:string|null;clip_json:string};
export type PodcastDatabase = SQL | SQLitePodcastDatabase;

/** Swift owns schema migration. The processor opens its tables in the same local
 * volume; all transactions below are synchronous and contain no network I/O. */
export class SQLitePodcastDatabase {
  readonly sqlite:Database;
  constructor(path:string) {
    this.sqlite=new Database(path,{strict:true});
    this.sqlite.exec("PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL");
  }
  ready():void {this.sqlite.query("SELECT id FROM podcast_jobs LIMIT 0").all();}
  close():void {this.sqlite.close();}

  claim(worker:string,allowBridge:boolean,now=Date.now()/1000):PodcastJob|undefined {
    return this.sqlite.transaction(()=>{
      const expired=this.sqlite.query<{kind:string;payload_json:string},[number,number]>("UPDATE podcast_jobs SET status='failed',error='Processing lease expired',lease_until=NULL,updated_at=? WHERE status='running' AND lease_until<? AND attempts>=3 RETURNING kind,payload_json").all(now,now);
      for(const row of expired)if(row.kind==="clip")this.mergeClip(String(JSON.parse(row.payload_json).clipId),{status:"failed",error:"Processing lease expired"},now);
      const row=this.sqlite.query<StoredJob,[string,number,number,number,number,number]>(`UPDATE podcast_jobs SET status='running',attempts=attempts+1,worker_id=?,lease_until=?,updated_at=?
        WHERE id=(SELECT id FROM podcast_jobs WHERE ((status='queued' AND available_at<=?) OR (status='running' AND lease_until<?)) AND (kind<>'bridge' OR ?) ORDER BY available_at,id LIMIT 1) RETURNING *`).get(worker,now+120,now,now,now,allowBridge?1:0);
      return row?{...row,payload_json:JSON.parse(row.payload_json)}:undefined;
    }).immediate();
  }
  heartbeat(id:string,worker:string,now=Date.now()/1000):boolean {
    return this.sqlite.query("UPDATE podcast_jobs SET lease_until=? WHERE id=? AND worker_id=? AND status='running' AND lease_until>? RETURNING id").get(now+120,id,worker,now)!==null;
  }
  complete(job:PodcastJob,worker:string,result:Record<string,unknown>,now=Date.now()/1000):boolean {
    return this.sqlite.transaction(()=>{
      const changed=this.sqlite.query("UPDATE podcast_jobs SET status='complete',result_json=?,error=NULL,lease_until=NULL,updated_at=? WHERE id=? AND worker_id=? AND status='running' AND lease_until>? RETURNING id").get(JSON.stringify(result),now,job.id,worker,now);
      if(changed&&job.kind==="clip")this.mergeClip(String(job.payload_json.clipId),{...result,status:"complete"},now);
      return changed!==null;
    }).immediate();
  }
  fail(job:PodcastJob,worker:string,error:string,now=Date.now()/1000):boolean {
    return this.sqlite.transaction(()=>{
      const failed=job.attempts>=3;
      const changed=this.sqlite.query("UPDATE podcast_jobs SET status=?,error=?,lease_until=NULL,available_at=?,updated_at=? WHERE id=? AND worker_id=? AND status='running' AND lease_until>? RETURNING id").get(failed?"failed":"queued",error,now+60,now,job.id,worker,now);
      if(changed&&failed&&job.kind==="clip")this.mergeClip(String(job.payload_json.clipId),{status:"failed",error},now);
      return changed!==null;
    }).immediate();
  }
  subscribedShows():Array<{id:string;show_json:PodcastShow}> {
    return this.sqlite.query<{id:string;show_json:string},[]>("SELECT DISTINCT s.id,s.show_json FROM podcast_shows s JOIN podcast_subscriptions sub ON sub.show_id=s.id ORDER BY s.id").all().map(row=>({...row,show_json:JSON.parse(row.show_json)}));
  }
  enqueueBridge(key:string,payload:Record<string,unknown>,now=Date.now()/1000):void {
    this.sqlite.query("INSERT INTO podcast_jobs(id,kind,dedupe_key,payload_json,available_at,created_at,updated_at) VALUES(?,'bridge',?,?,?,?,?) ON CONFLICT(dedupe_key) DO NOTHING").run(randomUUID(),key,JSON.stringify(payload),now,now,now);
  }
  subscribed(showId:string):boolean {return this.sqlite.query("SELECT show_id FROM podcast_subscriptions WHERE show_id=? LIMIT 1").get(showId)!==null;}
  show(showId:string):PodcastShow|undefined {
    const row=this.sqlite.query<{show_json:string},[string]>("SELECT show_json FROM podcast_shows WHERE id=?").get(showId);
    return row?JSON.parse(row.show_json):undefined;
  }
  episodes(showId:string):PodcastEpisode[] {
    return this.sqlite.query<{episode_json:string},[string]>("SELECT episode_json FROM podcast_episodes WHERE show_id=? ORDER BY published_at,id").all(showId).map(row=>JSON.parse(row.episode_json));
  }
  clip(id:string):{published_uri:string|null;clip_json:Record<string,unknown>}|undefined {
    const row=this.sqlite.query<ClipRow,[string]>("SELECT published_uri,clip_json FROM podcast_clips WHERE id=?").get(id);
    return row?{published_uri:row.published_uri,clip_json:JSON.parse(row.clip_json)}:undefined;
  }
  updateShow(show:PodcastShow,now=Date.now()/1000):void {
    this.sqlite.query("UPDATE podcast_shows SET show_json=?,updated_at=? WHERE id=?").run(JSON.stringify(show),now,show.id);
  }
  upsertRssEpisode(episode:PodcastEpisode,now=Date.now()/1000):void {
    this.sqlite.transaction(()=>{
      const previous=episode.guid?this.sqlite.query<{id:string;episode_json:string},[string,string]>("SELECT id,episode_json FROM podcast_episodes WHERE show_id=? AND guid=?").get(episode.showId,episode.guid):null;
      if(previous) {
        const old=JSON.parse(previous.episode_json) as PodcastEpisode;
        const merged={...episode,id:previous.id,sourceUri:old.sourceUri??episode.sourceUri,durationSeconds:episode.durationSeconds??old.durationSeconds,
          chapters:preservedChapters(episode,old),transcripts:episode.transcripts.length?episode.transcripts:old.transcripts};
        this.sqlite.query("UPDATE podcast_episodes SET episode_json=?,updated_at=? WHERE id=?").run(JSON.stringify(merged),now,previous.id);
      } else this.sqlite.query("INSERT INTO podcast_episodes(id,show_id,guid,episode_json,published_at,updated_at) VALUES(?,?,?,?,?,?)").run(episode.id,episode.showId,episode.guid??null,JSON.stringify(episode),publishedAt(episode),now);
    }).immediate();
  }
  upsertProtocolEpisode(episode:PodcastEpisode,alias:string,now=Date.now()/1000):void {
    this.sqlite.transaction(()=>{
      const matching=episode.guid?this.sqlite.query<{id:string},[string,string]>("SELECT id FROM podcast_episodes WHERE show_id=? AND guid=?").get(episode.showId,episode.guid):null;
      const id=matching?.id??episode.id;
      const previous=this.sqlite.query<{episode_json:string},[string]>("SELECT episode_json FROM podcast_episodes WHERE id=?").get(id);
      const merged={...episode,id,chapters:previous?preservedChapters(episode,JSON.parse(previous.episode_json)):episode.chapters??[]};
      this.sqlite.query(`INSERT INTO podcast_episodes(id,show_id,guid,episode_json,published_at,updated_at) VALUES(?,?,?,?,?,?)
        ON CONFLICT(id) DO UPDATE SET episode_json=excluded.episode_json,updated_at=excluded.updated_at`).run(id,episode.showId,episode.guid??null,JSON.stringify(merged),publishedAt(episode),now);
      this.addAlias(alias,id,"episode");
    }).immediate();
  }
  addAlias(alias:string,id:string,kind:"show"|"episode"):void {
    this.sqlite.query("INSERT INTO podcast_aliases(alias,canonical_id,entity_kind) VALUES(?,?,?) ON CONFLICT(alias) DO NOTHING").run(alias,id,kind);
  }
  hasAlias(alias:string):boolean {return this.sqlite.query("SELECT alias FROM podcast_aliases WHERE alias=?").get(alias)!==null;}
  mergeEpisode(id:string,patch:Record<string,unknown>,now=Date.now()/1000):void {
    this.sqlite.transaction(()=>{
      const row=this.sqlite.query<{episode_json:string},[string]>("SELECT episode_json FROM podcast_episodes WHERE id=?").get(id);
      if(row)this.sqlite.query("UPDATE podcast_episodes SET episode_json=?,updated_at=? WHERE id=?").run(JSON.stringify({...JSON.parse(row.episode_json),...patch}),now,id);
    }).immediate();
  }
  mergeShow(id:string,patch:Record<string,unknown>,now=Date.now()/1000):void {
    this.sqlite.transaction(()=>{
      const row=this.sqlite.query<{show_json:string},[string]>("SELECT show_json FROM podcast_shows WHERE id=?").get(id);
      if(row)this.sqlite.query("UPDATE podcast_shows SET show_json=?,updated_at=? WHERE id=?").run(JSON.stringify({...JSON.parse(row.show_json),...patch}),now,id);
    }).immediate();
  }
  private mergeClip(id:string,patch:Record<string,unknown>,now:number):void {
    const row=this.sqlite.query<ClipRow,[string]>("SELECT published_uri,clip_json FROM podcast_clips WHERE id=?").get(id);
    if(row)this.sqlite.query("UPDATE podcast_clips SET clip_json=?,updated_at=? WHERE id=?").run(JSON.stringify({...JSON.parse(row.clip_json),...patch}),now,id);
  }
}

function publishedAt(episode:PodcastEpisode):number {
  const seconds=Date.parse(episode.publishedAt)/1000;
  return Number.isFinite(seconds)?seconds:0;
}
function preservedChapters(episode:PodcastEpisode,previous:PodcastEpisode) {
  return !episode.chapters?.length&&(episode.chapterSourceUrl??null)===(previous.chapterSourceUrl??null)?previous.chapters??[]:episode.chapters??[];
}
