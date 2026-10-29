export namespace activity {
	
	export class ExecStatSummary {
	    skill: string;
	    runs: number;
	    error_runs: number;
	    tripped_runs: number;
	    total_duration_ms: number;
	    avg_duration_ms: number;
	    max_duration_ms: number;
	    total_turns: number;
	    avg_turns: number;
	    max_turns_used: number;
	
	    static createFrom(source: any = {}) {
	        return new ExecStatSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.skill = source["skill"];
	        this.runs = source["runs"];
	        this.error_runs = source["error_runs"];
	        this.tripped_runs = source["tripped_runs"];
	        this.total_duration_ms = source["total_duration_ms"];
	        this.avg_duration_ms = source["avg_duration_ms"];
	        this.max_duration_ms = source["max_duration_ms"];
	        this.total_turns = source["total_turns"];
	        this.avg_turns = source["avg_turns"];
	        this.max_turns_used = source["max_turns_used"];
	    }
	}
	export class FeatureUsageStat {
	    feature_key: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new FeatureUsageStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.feature_key = source["feature_key"];
	        this.count = source["count"];
	    }
	}

}

export namespace agentcfg {
	
	export class PersonaRef {
	    kind: string;
	    ref: string;
	
	    static createFrom(source: any = {}) {
	        return new PersonaRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.ref = source["ref"];
	    }
	}

}

export namespace app {
	
	export class AuthorAliasGroupDTO {
	    canonical: string;
	    aliases: string[];
	
	    static createFrom(source: any = {}) {
	        return new AuthorAliasGroupDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.canonical = source["canonical"];
	        this.aliases = source["aliases"];
	    }
	}
	export class CharacterGroupInput {
	    name: string;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new CharacterGroupInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	    }
	}
	export class CharacterGroupMembershipInput {
	    character_ids: number[];
	    add_group_ids: number[];
	    remove_group_ids: number[];
	
	    static createFrom(source: any = {}) {
	        return new CharacterGroupMembershipInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.character_ids = source["character_ids"];
	        this.add_group_ids = source["add_group_ids"];
	        this.remove_group_ids = source["remove_group_ids"];
	    }
	}
	export class ChatInput {
	    session_id: string;
	    novel_id: number;
	    message: string;
	    provider_name: string;
	    model_id: string;
	    reasoning_effort: string;
	    persona?: agentcfg.PersonaRef;
	
	    static createFrom(source: any = {}) {
	        return new ChatInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.novel_id = source["novel_id"];
	        this.message = source["message"];
	        this.provider_name = source["provider_name"];
	        this.model_id = source["model_id"];
	        this.reasoning_effort = source["reasoning_effort"];
	        this.persona = this.convertValues(source["persona"], agentcfg.PersonaRef);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ChatResult {
	    session_id: string;
	    turn_id: number;
	    final_text: string;
	
	    static createFrom(source: any = {}) {
	        return new ChatResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.turn_id = source["turn_id"];
	        this.final_text = source["final_text"];
	    }
	}
	export class CommitFileListResult {
	    commit?: git.CommitInfo;
	    files: git.FileEntry[];
	
	    static createFrom(source: any = {}) {
	        return new CommitFileListResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.commit = this.convertValues(source["commit"], git.CommitInfo);
	        this.files = this.convertValues(source["files"], git.FileEntry);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CompressInput {
	    session_id: string;
	    provider_name: string;
	    model_id: string;
	
	    static createFrom(source: any = {}) {
	        return new CompressInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.provider_name = source["provider_name"];
	        this.model_id = source["model_id"];
	    }
	}
	export class CompressResult {
	    turn_id: number;
	
	    static createFrom(source: any = {}) {
	        return new CompressResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.turn_id = source["turn_id"];
	    }
	}
	export class ComputeStyleStatsInput {
	    sample_ids: number[];
	
	    static createFrom(source: any = {}) {
	        return new ComputeStyleStatsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sample_ids = source["sample_ids"];
	    }
	}
	export class CreateArcNodeInput {
	    story_arc_id: number;
	    title: string;
	    description?: string;
	    target_reading_number: number;
	
	    static createFrom(source: any = {}) {
	        return new CreateArcNodeInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.story_arc_id = source["story_arc_id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.target_reading_number = source["target_reading_number"];
	    }
	}
	export class CreateChapterInput {
	    novel_id: number;
	    title: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateChapterInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.title = source["title"];
	    }
	}
	export class CreateCharacterInput {
	    name: string;
	    description?: string;
	    personality?: string;
	    abilities?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateCharacterInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.personality = source["personality"];
	        this.abilities = source["abilities"];
	    }
	}
	export class CreateLocationInput {
	    name: string;
	    location_type?: string;
	    description?: string;
	    detail_json?: string;
	    parent_location_id?: number;
	    tags?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateLocationInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.location_type = source["location_type"];
	        this.description = source["description"];
	        this.detail_json = source["detail_json"];
	        this.parent_location_id = source["parent_location_id"];
	        this.tags = source["tags"];
	    }
	}
	export class CreateNovelInput {
	    title: string;
	    description?: string;
	    genre?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateNovelInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.description = source["description"];
	        this.genre = source["genre"];
	    }
	}
	export class CreateNovelSettingInput {
	    category: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateNovelSettingInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.category = source["category"];
	        this.content = source["content"];
	    }
	}
	export class CreatePreferenceInput {
	    is_global: boolean;
	    category: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new CreatePreferenceInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.is_global = source["is_global"];
	        this.category = source["category"];
	        this.content = source["content"];
	    }
	}
	export class CreateReaderPerspectiveInput {
	    type: string;
	    content: string;
	    planted_chapter_id: number;
	    related_truth?: string;
	    revealed_chapter_id?: number;
	
	    static createFrom(source: any = {}) {
	        return new CreateReaderPerspectiveInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.content = source["content"];
	        this.planted_chapter_id = source["planted_chapter_id"];
	        this.related_truth = source["related_truth"];
	        this.revealed_chapter_id = source["revealed_chapter_id"];
	    }
	}
	export class CreateStoryArcInput {
	    name: string;
	    arc_type: string;
	    description?: string;
	    importance?: number;
	
	    static createFrom(source: any = {}) {
	        return new CreateStoryArcInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.arc_type = source["arc_type"];
	        this.description = source["description"];
	        this.importance = source["importance"];
	    }
	}
	export class CreateStyleProfileInput {
	    novel_id: number;
	    name: string;
	    description: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateStyleProfileInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.content = source["content"];
	    }
	}
	export class CreateStyleSampleInput {
	    novel_id: number;
	    is_global: boolean;
	    name: string;
	    content: string;
	    tags: string[];
	
	    static createFrom(source: any = {}) {
	        return new CreateStyleSampleInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.is_global = source["is_global"];
	        this.name = source["name"];
	        this.content = source["content"];
	        this.tags = source["tags"];
	    }
	}
	export class CreateTimelineEntryInput {
	    category: string;
	    title: string;
	    content?: string;
	    detail_json?: string;
	    target_reading_number: number;
	    importance?: number;
	    source_chapter_id?: number;
	    source?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateTimelineEntryInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.category = source["category"];
	        this.title = source["title"];
	        this.content = source["content"];
	        this.detail_json = source["detail_json"];
	        this.target_reading_number = source["target_reading_number"];
	        this.importance = source["importance"];
	        this.source_chapter_id = source["source_chapter_id"];
	        this.source = source["source"];
	    }
	}
	export class DeleteSkillInput {
	    novel_id: number;
	    name: string;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new DeleteSkillInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.name = source["name"];
	        this.source = source["source"];
	    }
	}
	export class DeleteStyleProfileInput {
	    id: number;
	
	    static createFrom(source: any = {}) {
	        return new DeleteStyleProfileInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	    }
	}
	export class DeleteStyleSampleInput {
	    id: number;
	
	    static createFrom(source: any = {}) {
	        return new DeleteStyleSampleInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	    }
	}
	export class ExtractStyleInput {
	    task_id: string;
	    sample_ids: number[];
	    provider_name: string;
	    model_id: string;
	    reasoning_effort: string;
	    novel_id?: number;
	
	    static createFrom(source: any = {}) {
	        return new ExtractStyleInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.sample_ids = source["sample_ids"];
	        this.provider_name = source["provider_name"];
	        this.model_id = source["model_id"];
	        this.reasoning_effort = source["reasoning_effort"];
	        this.novel_id = source["novel_id"];
	    }
	}
	export class GenerateCoverInput {
	    novel_id: number;
	    prompt: string;
	    provider_name: string;
	    model: string;
	    size: string;
	    style_preset: string;
	    negative_prompt: string;
	
	    static createFrom(source: any = {}) {
	        return new GenerateCoverInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.prompt = source["prompt"];
	        this.provider_name = source["provider_name"];
	        this.model = source["model"];
	        this.size = source["size"];
	        this.style_preset = source["style_preset"];
	        this.negative_prompt = source["negative_prompt"];
	    }
	}
	export class GetAuthorAliasesResult {
	    version: number;
	    groups: AuthorAliasGroupDTO[];
	
	    static createFrom(source: any = {}) {
	        return new GetAuthorAliasesResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.groups = this.convertValues(source["groups"], AuthorAliasGroupDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class GetChapterContentsInput {
	    novel_id: number;
	    chapter_ids: number[];
	
	    static createFrom(source: any = {}) {
	        return new GetChapterContentsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.chapter_ids = source["chapter_ids"];
	    }
	}
	export class GetChapterContentsResult {
	    content: string;
	    chapter_count: number;
	
	    static createFrom(source: any = {}) {
	        return new GetChapterContentsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	        this.chapter_count = source["chapter_count"];
	    }
	}
	export class GetCharacterGroupMembersInput {
	    novel_id: number;
	    page: number;
	    size: number;
	    search: string;
	    group_id: number;
	    character_id: number;
	
	    static createFrom(source: any = {}) {
	        return new GetCharacterGroupMembersInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.page = source["page"];
	        this.size = source["size"];
	        this.search = source["search"];
	        this.group_id = source["group_id"];
	        this.character_id = source["character_id"];
	    }
	}
	export class GetCharacterGroupsInput {
	    novel_id: number;
	    page: number;
	    size: number;
	    search: string;
	
	    static createFrom(source: any = {}) {
	        return new GetCharacterGroupsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.page = source["page"];
	        this.size = source["size"];
	        this.search = source["search"];
	    }
	}
	export class GetSessionsInput {
	    novel_id: number;
	    page: number;
	    size: number;
	    search: string;
	
	    static createFrom(source: any = {}) {
	        return new GetSessionsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.page = source["page"];
	        this.size = source["size"];
	        this.search = source["search"];
	    }
	}
	export class ImportNovelInput {
	    file_path: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportNovelInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file_path = source["file_path"];
	    }
	}
	export class ImportWithLLMInput {
	    file_path: string;
	    provider_name: string;
	    model_id: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportWithLLMInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file_path = source["file_path"];
	        this.provider_name = source["provider_name"];
	        this.model_id = source["model_id"];
	    }
	}
	export class InstallRemoteSkillInput {
	    name: string;
	    target: string;
	    novel_id: number;
	    expected_content?: string;
	
	    static createFrom(source: any = {}) {
	        return new InstallRemoteSkillInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.target = source["target"];
	        this.novel_id = source["novel_id"];
	        this.expected_content = source["expected_content"];
	    }
	}
	export class ListRemoteSkillsInput {
	    page: number;
	    size: number;
	    query: string;
	
	    static createFrom(source: any = {}) {
	        return new ListRemoteSkillsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.page = source["page"];
	        this.size = source["size"];
	        this.query = source["query"];
	    }
	}
	export class ListSkillsInput {
	    novel_id: number;
	
	    static createFrom(source: any = {}) {
	        return new ListSkillsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	    }
	}
	export class ListSlashCommandsInput {
	    novel_id: number;
	
	    static createFrom(source: any = {}) {
	        return new ListSlashCommandsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	    }
	}
	export class ListStyleProfilesInput {
	    novel_id: number;
	
	    static createFrom(source: any = {}) {
	        return new ListStyleProfilesInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	    }
	}
	export class ListStyleSamplesInput {
	    novel_id: number;
	    page: number;
	    size: number;
	    search: string;
	
	    static createFrom(source: any = {}) {
	        return new ListStyleSamplesInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.page = source["page"];
	        this.size = source["size"];
	        this.search = source["search"];
	    }
	}
	export class MergeMentalSkillsInput {
	    novel_id: number;
	    names: string[];
	    canonical_author: string;
	    alias_map: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new MergeMentalSkillsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.names = source["names"];
	        this.canonical_author = source["canonical_author"];
	        this.alias_map = source["alias_map"];
	    }
	}
	export class PreferenceResult {
	    global: preference.PreferenceItem[];
	    novel: preference.PreferenceItem[];
	    token_count: number;
	    over_budget: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PreferenceResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.global = this.convertValues(source["global"], preference.PreferenceItem);
	        this.novel = this.convertValues(source["novel"], preference.PreferenceItem);
	        this.token_count = source["token_count"];
	        this.over_budget = source["over_budget"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RebuildAggregatedInput {
	    novel_id: number;
	
	    static createFrom(source: any = {}) {
	        return new RebuildAggregatedInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	    }
	}
	export class RebuildAggregatedResult {
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new RebuildAggregatedResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.count = source["count"];
	    }
	}
	export class SaveAuthorAliasesInput {
	    groups: AuthorAliasGroupDTO[];
	
	    static createFrom(source: any = {}) {
	        return new SaveAuthorAliasesInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.groups = this.convertValues(source["groups"], AuthorAliasGroupDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SaveContentInput {
	    novel_id: number;
	    path: string;
	    content: string;
	    expected_content?: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveContentInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.path = source["path"];
	        this.content = source["content"];
	        this.expected_content = source["expected_content"];
	    }
	}
	export class SavePersonaSettingInput {
	    enabled: boolean;
	    kind: string;
	    ref: string;
	
	    static createFrom(source: any = {}) {
	        return new SavePersonaSettingInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.kind = source["kind"];
	        this.ref = source["ref"];
	    }
	}
	export class SaveSettingsInput {
	
	
	    static createFrom(source: any = {}) {
	        return new SaveSettingsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class SaveWritingConfigInput {
	    auto_score_enabled: boolean;
	    score_threshold: number;
	    score_max_retries: number;
	
	    static createFrom(source: any = {}) {
	        return new SaveWritingConfigInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.auto_score_enabled = source["auto_score_enabled"];
	        this.score_threshold = source["score_threshold"];
	        this.score_max_retries = source["score_max_retries"];
	    }
	}
	export class SessionDetail {
	    session_id: string;
	    novel_id: number;
	    title: string;
	    model: string;
	    reasoning_effort: string;
	    active_version: number;
	    last_turn_id: number;
	    usage?: number[];
	    created_at: string;
	    updated_at: string;
	
	    static createFrom(source: any = {}) {
	        return new SessionDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.novel_id = source["novel_id"];
	        this.title = source["title"];
	        this.model = source["model"];
	        this.reasoning_effort = source["reasoning_effort"];
	        this.active_version = source["active_version"];
	        this.last_turn_id = source["last_turn_id"];
	        this.usage = source["usage"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class SessionMeta {
	    session_id: string;
	    title: string;
	    updated_at: string;
	
	    static createFrom(source: any = {}) {
	        return new SessionMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.title = source["title"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class SetActiveNovelInput {
	    novel_id: number;
	
	    static createFrom(source: any = {}) {
	        return new SetActiveNovelInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	    }
	}
	export class SetEnabledSkillsInput {
	    novel_id: number;
	    names: string[];
	
	    static createFrom(source: any = {}) {
	        return new SetEnabledSkillsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.names = source["names"];
	    }
	}
	export class SettingResult {
	    items: setting.SettingItem[];
	    token_count: number;
	    over_budget: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SettingResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], setting.SettingItem);
	        this.token_count = source["token_count"];
	        this.over_budget = source["over_budget"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SlashCommand {
	    name: string;
	    description: string;
	    type: string;
	    use_when_hint: string;
	
	    static createFrom(source: any = {}) {
	        return new SlashCommand(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.type = source["type"];
	        this.use_when_hint = source["use_when_hint"];
	    }
	}
	export class StartupState {
	    phase: string;
	    error?: string;
	    version: number;
	
	    static createFrom(source: any = {}) {
	        return new StartupState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phase = source["phase"];
	        this.error = source["error"];
	        this.version = source["version"];
	    }
	}
	export class TestConnectionInput {
	    provider_name: string;
	    chat_url: string;
	    api_key: string;
	    model_id: string;
	
	    static createFrom(source: any = {}) {
	        return new TestConnectionInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider_name = source["provider_name"];
	        this.chat_url = source["chat_url"];
	        this.api_key = source["api_key"];
	        this.model_id = source["model_id"];
	    }
	}
	export class UpdateArcNodeInput {
	    title: string;
	    description: string;
	    target_reading_number: number;
	    actual_chapter_id?: number;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateArcNodeInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.description = source["description"];
	        this.target_reading_number = source["target_reading_number"];
	        this.actual_chapter_id = source["actual_chapter_id"];
	        this.status = source["status"];
	    }
	}
	export class UpdateChapterPlanInput {
	    scope: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateChapterPlanInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scope = source["scope"];
	        this.content = source["content"];
	    }
	}
	export class UpdateCharacterInput {
	    name: string;
	    description: string;
	    abilities: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateCharacterInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.abilities = source["abilities"];
	    }
	}
	export class UpdateLocationInput {
	    name: string;
	    location_type: string;
	    description: string;
	    parent_location_id?: number;
	    tags: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateLocationInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.location_type = source["location_type"];
	        this.description = source["description"];
	        this.parent_location_id = source["parent_location_id"];
	        this.tags = source["tags"];
	    }
	}
	export class UpdateNovelInput {
	    title: string;
	    description: string;
	    genre: string;
	    kind?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateNovelInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.description = source["description"];
	        this.genre = source["genre"];
	        this.kind = source["kind"];
	    }
	}
	export class UpdateNovelSettingInput {
	    category: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateNovelSettingInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.category = source["category"];
	        this.content = source["content"];
	    }
	}
	export class UpdatePreferenceInput {
	    category: string;
	    content: string;
	    is_global: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UpdatePreferenceInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.category = source["category"];
	        this.content = source["content"];
	        this.is_global = source["is_global"];
	    }
	}
	export class UpdateReaderPerspectiveInput {
	    type: string;
	    content: string;
	    planted_chapter_id?: number;
	    related_truth: string;
	    revealed_chapter_id?: number;
	
	    static createFrom(source: any = {}) {
	        return new UpdateReaderPerspectiveInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.content = source["content"];
	        this.planted_chapter_id = source["planted_chapter_id"];
	        this.related_truth = source["related_truth"];
	        this.revealed_chapter_id = source["revealed_chapter_id"];
	    }
	}
	export class UpdateStoryArcInput {
	    name: string;
	    description: string;
	    arc_type: string;
	    importance: number;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateStoryArcInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.arc_type = source["arc_type"];
	        this.importance = source["importance"];
	        this.status = source["status"];
	    }
	}
	export class UpdateStyleSampleInput {
	    id: number;
	    name: string;
	    content: string;
	    tags: string[];
	    is_global: boolean;
	    novel_id: number;
	
	    static createFrom(source: any = {}) {
	        return new UpdateStyleSampleInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.content = source["content"];
	        this.tags = source["tags"];
	        this.is_global = source["is_global"];
	        this.novel_id = source["novel_id"];
	    }
	}
	export class UpdateTimelineEntryInput {
	    title: string;
	    content: string;
	    target_reading_number: number;
	    importance: number;
	    status: string;
	    resolved_chapter_id?: number;
	
	    static createFrom(source: any = {}) {
	        return new UpdateTimelineEntryInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.content = source["content"];
	        this.target_reading_number = source["target_reading_number"];
	        this.importance = source["importance"];
	        this.status = source["status"];
	        this.resolved_chapter_id = source["resolved_chapter_id"];
	    }
	}
	export class UpsertVoiceDimensionInput {
	    novel_id: number;
	    dimension: string;
	    content: string;
	    metrics: string;
	    evidence: string;
	
	    static createFrom(source: any = {}) {
	        return new UpsertVoiceDimensionInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.dimension = source["dimension"];
	        this.content = source["content"];
	        this.metrics = source["metrics"];
	        this.evidence = source["evidence"];
	    }
	}
	export class VoiceDimensionView {
	    dimension: string;
	    present: boolean;
	    content: string;
	    metrics: string;
	    evidence: string;
	    source: string;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new VoiceDimensionView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dimension = source["dimension"];
	        this.present = source["present"];
	        this.content = source["content"];
	        this.metrics = source["metrics"];
	        this.evidence = source["evidence"];
	        this.source = source["source"];
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace apperr {
	
	export class Result__github_com_novelgo_novelgo_internal_storage_PageResult_github_com_novelgo_novelgo_internal_skill_remote_RemoteSkillMeta__ {
	    data?: storage.PageResult_github_com_novelgo_novelgo_internal_skill_remote_RemoteSkillMeta_;
	    err_code: string;
	    err_msg?: string;
	
	    static createFrom(source: any = {}) {
	        return new Result__github_com_novelgo_novelgo_internal_storage_PageResult_github_com_novelgo_novelgo_internal_skill_remote_RemoteSkillMeta__(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.data = this.convertValues(source["data"], storage.PageResult_github_com_novelgo_novelgo_internal_skill_remote_RemoteSkillMeta_);
	        this.err_code = source["err_code"];
	        this.err_msg = source["err_msg"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result_string_ {
	    data: string;
	    err_code: string;
	    err_msg?: string;
	
	    static createFrom(source: any = {}) {
	        return new Result_string_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.data = source["data"];
	        this.err_code = source["err_code"];
	        this.err_msg = source["err_msg"];
	    }
	}
	export class Result_struct____ {
	    // Go type: struct {}
	    data: any;
	    err_code: string;
	    err_msg?: string;
	
	    static createFrom(source: any = {}) {
	        return new Result_struct____(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.data = this.convertValues(source["data"], Object);
	        this.err_code = source["err_code"];
	        this.err_msg = source["err_msg"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace chapter {
	
	export class Chapter {
	    id: number;
	    novel_id: number;
	    volume_id?: number;
	    sort_order: number;
	    title: string;
	    summary: string;
	    word_count: number;
	    quality_score?: number;
	    quality_report: string;
	    // Go type: time
	    quality_scored_at?: any;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    file_path: string;
	    outline_file_path: string;
	    reading_number: number;
	
	    static createFrom(source: any = {}) {
	        return new Chapter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.volume_id = source["volume_id"];
	        this.sort_order = source["sort_order"];
	        this.title = source["title"];
	        this.summary = source["summary"];
	        this.word_count = source["word_count"];
	        this.quality_score = source["quality_score"];
	        this.quality_report = source["quality_report"];
	        this.quality_scored_at = this.convertValues(source["quality_scored_at"], null);
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.file_path = source["file_path"];
	        this.outline_file_path = source["outline_file_path"];
	        this.reading_number = source["reading_number"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DeleteResult {
	    deleted: boolean;
	    references: deletion.Blocker[];
	
	    static createFrom(source: any = {}) {
	        return new DeleteResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deleted = source["deleted"];
	        this.references = this.convertValues(source["references"], deletion.Blocker);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PlaceInput {
	    novel_id: number;
	    source_chapter_id?: number;
	    title?: string;
	    target_volume_id?: number;
	    before_chapter_id?: number;
	
	    static createFrom(source: any = {}) {
	        return new PlaceInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.source_chapter_id = source["source_chapter_id"];
	        this.title = source["title"];
	        this.target_volume_id = source["target_volume_id"];
	        this.before_chapter_id = source["before_chapter_id"];
	    }
	}

}

export namespace character {
	
	export class Character {
	    id: number;
	    novel_id: number;
	    name: string;
	    description: string;
	    personality: string;
	    abilities: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    aliases: string;
	    gender: string;
	    birth_year: string;
	    appearance_anchors: string;
	    alive?: boolean;
	    location_ref: string;
	    faction: string;
	    role: string;
	    capability_ceiling: string;
	    sealed: boolean;
	    voice_traits: string;
	    volume_id?: number;
	    // Go type: time
	    changed_at?: any;
	    changed_in_volume_id?: number;
	    changed_in_chapter_id?: number;
	
	    static createFrom(source: any = {}) {
	        return new Character(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.personality = source["personality"];
	        this.abilities = source["abilities"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.aliases = source["aliases"];
	        this.gender = source["gender"];
	        this.birth_year = source["birth_year"];
	        this.appearance_anchors = source["appearance_anchors"];
	        this.alive = source["alive"];
	        this.location_ref = source["location_ref"];
	        this.faction = source["faction"];
	        this.role = source["role"];
	        this.capability_ceiling = source["capability_ceiling"];
	        this.sealed = source["sealed"];
	        this.voice_traits = source["voice_traits"];
	        this.volume_id = source["volume_id"];
	        this.changed_at = this.convertValues(source["changed_at"], null);
	        this.changed_in_volume_id = source["changed_in_volume_id"];
	        this.changed_in_chapter_id = source["changed_in_chapter_id"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CharacterRelation {
	    id: number;
	    novel_id: number;
	    source_character_id: number;
	    target_character_id: number;
	    relation_describe: string;
	    relation_kind: string;
	    description: string;
	    chapter_id?: number;
	    is_current: boolean;
	    // Go type: time
	    created_at: any;
	    volume_id?: number;
	    // Go type: time
	    changed_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new CharacterRelation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.source_character_id = source["source_character_id"];
	        this.target_character_id = source["target_character_id"];
	        this.relation_describe = source["relation_describe"];
	        this.relation_kind = source["relation_kind"];
	        this.description = source["description"];
	        this.chapter_id = source["chapter_id"];
	        this.is_current = source["is_current"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.volume_id = source["volume_id"];
	        this.changed_at = this.convertValues(source["changed_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Group {
	    id: number;
	    novel_id: number;
	    name: string;
	    description: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Group(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class GroupMember {
	    id: number;
	    novel_id: number;
	    group_id: number;
	    character_id: number;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new GroupMember(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.group_id = source["group_id"];
	        this.character_id = source["character_id"];
	        this.created_at = this.convertValues(source["created_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class GroupView {
	    id: number;
	    novel_id: number;
	    name: string;
	    description: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    member_count: number;
	
	    static createFrom(source: any = {}) {
	        return new GroupView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.member_count = source["member_count"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace config {
	
	export class AppSettings {
	    ID: number;
	    last_novel_id: number;
	    selected_model_key: string;
	    reasoning_effort: string;
	    approval_mode: string;
	    last_session_id: string;
	    user_name: string;
	    git_name: string;
	    git_email: string;
	    dismissed_version: string;
	    // Go type: time
	    last_update_check_at: any;
	    image_model: string;
	    image_url: string;
	
	    static createFrom(source: any = {}) {
	        return new AppSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.last_novel_id = source["last_novel_id"];
	        this.selected_model_key = source["selected_model_key"];
	        this.reasoning_effort = source["reasoning_effort"];
	        this.approval_mode = source["approval_mode"];
	        this.last_session_id = source["last_session_id"];
	        this.user_name = source["user_name"];
	        this.git_name = source["git_name"];
	        this.git_email = source["git_email"];
	        this.dismissed_version = source["dismissed_version"];
	        this.last_update_check_at = this.convertValues(source["last_update_check_at"], null);
	        this.image_model = source["image_model"];
	        this.image_url = source["image_url"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace deletion {
	
	export class Blocker {
	    kind: string;
	    id: number;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new Blocker(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.id = source["id"];
	        this.label = source["label"];
	    }
	}

}

export namespace git {
	
	export class CommitInfo {
	    hash: string;
	    shortHash: string;
	    message: string;
	    // Go type: time
	    time: any;
	    authorName: string;
	    authorEmail: string;
	    filesChanged: number;
	    insertions: number;
	    deletions: number;
	
	    static createFrom(source: any = {}) {
	        return new CommitInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.shortHash = source["shortHash"];
	        this.message = source["message"];
	        this.time = this.convertValues(source["time"], null);
	        this.authorName = source["authorName"];
	        this.authorEmail = source["authorEmail"];
	        this.filesChanged = source["filesChanged"];
	        this.insertions = source["insertions"];
	        this.deletions = source["deletions"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FileDiff {
	    path: string;
	    changeType: string;
	    original: string;
	    modified: string;
	
	    static createFrom(source: any = {}) {
	        return new FileDiff(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.changeType = source["changeType"];
	        this.original = source["original"];
	        this.modified = source["modified"];
	    }
	}
	export class FileEntry {
	    path: string;
	    changeType: string;
	    oldPath: string;
	
	    static createFrom(source: any = {}) {
	        return new FileEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.changeType = source["changeType"];
	        this.oldPath = source["oldPath"];
	    }
	}

}

export namespace imp {
	
	export class SkippedChapter {
	    title: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new SkippedChapter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.reason = source["reason"];
	    }
	}
	export class ImportResult {
	    novel_id: number;
	    title: string;
	    chapter_count: number;
	    skipped_count: number;
	    skipped_chapters: SkippedChapter[];
	    needs_llm: boolean;
	    file_path?: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.title = source["title"];
	        this.chapter_count = source["chapter_count"];
	        this.skipped_count = source["skipped_count"];
	        this.skipped_chapters = this.convertValues(source["skipped_chapters"], SkippedChapter);
	        this.needs_llm = source["needs_llm"];
	        this.file_path = source["file_path"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace llm {
	
	export class AvailableModel {
	    Key: string;
	    ModelID: string;
	    ProviderKey: string;
	    ProviderName: string;
	    ModelName: string;
	    ContextWindow: number;
	    MaxOutputTokens: number;
	    SupportsThinking: boolean;
	    ReasoningLevels: string[];
	    SupportsVision: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AvailableModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Key = source["Key"];
	        this.ModelID = source["ModelID"];
	        this.ProviderKey = source["ProviderKey"];
	        this.ProviderName = source["ProviderName"];
	        this.ModelName = source["ModelName"];
	        this.ContextWindow = source["ContextWindow"];
	        this.MaxOutputTokens = source["MaxOutputTokens"];
	        this.SupportsThinking = source["SupportsThinking"];
	        this.ReasoningLevels = source["ReasoningLevels"];
	        this.SupportsVision = source["SupportsVision"];
	    }
	}
	export class ModelInfo {
	    id: string;
	    name: string;
	    context_window: number;
	    max_output_tokens: number;
	    supports_thinking: boolean;
	    reasoning_levels?: string[];
	    supports_vision: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ModelInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.context_window = source["context_window"];
	        this.max_output_tokens = source["max_output_tokens"];
	        this.supports_thinking = source["supports_thinking"];
	        this.reasoning_levels = source["reasoning_levels"];
	        this.supports_vision = source["supports_vision"];
	    }
	}
	export class ProviderView {
	    key: string;
	    name: string;
	    chat_url: string;
	    api_key: string;
	    platform_url: string;
	    help_text: string;
	    temperature: number;
	    source: string;
	    builtin_models: ModelInfo[];
	    custom_models: ModelInfo[];
	
	    static createFrom(source: any = {}) {
	        return new ProviderView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.name = source["name"];
	        this.chat_url = source["chat_url"];
	        this.api_key = source["api_key"];
	        this.platform_url = source["platform_url"];
	        this.help_text = source["help_text"];
	        this.temperature = source["temperature"];
	        this.source = source["source"];
	        this.builtin_models = this.convertValues(source["builtin_models"], ModelInfo);
	        this.custom_models = this.convertValues(source["custom_models"], ModelInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LLMConfigView {
	    providers: ProviderView[];
	
	    static createFrom(source: any = {}) {
	        return new LLMConfigView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.providers = this.convertValues(source["providers"], ProviderView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class TestConnectionResult {
	    url: string;
	    warning: string;
	
	    static createFrom(source: any = {}) {
	        return new TestConnectionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.warning = source["warning"];
	    }
	}

}

export namespace location {
	
	export class Location {
	    id: number;
	    novel_id: number;
	    name: string;
	    location_type: string;
	    description: string;
	    detail_json: string;
	    parent_location_id?: number;
	    tags: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    state: string;
	    controller: string;
	    volume_id?: number;
	    // Go type: time
	    changed_at?: any;
	    changed_in_volume_id?: number;
	    changed_in_chapter_id?: number;
	
	    static createFrom(source: any = {}) {
	        return new Location(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.name = source["name"];
	        this.location_type = source["location_type"];
	        this.description = source["description"];
	        this.detail_json = source["detail_json"];
	        this.parent_location_id = source["parent_location_id"];
	        this.tags = source["tags"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.state = source["state"];
	        this.controller = source["controller"];
	        this.volume_id = source["volume_id"];
	        this.changed_at = this.convertValues(source["changed_at"], null);
	        this.changed_in_volume_id = source["changed_in_volume_id"];
	        this.changed_in_chapter_id = source["changed_in_chapter_id"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LocationRelation {
	    id: number;
	    novel_id: number;
	    location_a_id: number;
	    location_b_id: number;
	    relation_type: string;
	    description: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new LocationRelation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.location_a_id = source["location_a_id"];
	        this.location_b_id = source["location_b_id"];
	        this.relation_type = source["relation_type"];
	        this.description = source["description"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace novel {
	
	export class Novel {
	    id: number;
	    title: string;
	    genre: string;
	    description: string;
	    kind: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Novel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.genre = source["genre"];
	        this.description = source["description"];
	        this.kind = source["kind"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace pattern {
	
	export class BatchTrace {
	    index: number;
	    input_count: number;
	    output_count: number;
	    approx_tokens: number;
	
	    static createFrom(source: any = {}) {
	        return new BatchTrace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.input_count = source["input_count"];
	        this.output_count = source["output_count"];
	        this.approx_tokens = source["approx_tokens"];
	    }
	}
	export class BoundaryHint {
	    start_chapter_id: number;
	    end_chapter_id: number;
	    hint: string;
	
	    static createFrom(source: any = {}) {
	        return new BoundaryHint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start_chapter_id = source["start_chapter_id"];
	        this.end_chapter_id = source["end_chapter_id"];
	        this.hint = source["hint"];
	    }
	}
	export class ChapterSummaryItem {
	    chapter_id: number;
	    summary: string;
	
	    static createFrom(source: any = {}) {
	        return new ChapterSummaryItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.chapter_id = source["chapter_id"];
	        this.summary = source["summary"];
	    }
	}
	export class Chunk {
	    name: string;
	    start_chapter_id: number;
	    end_chapter_id: number;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new Chunk(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.start_chapter_id = source["start_chapter_id"];
	        this.end_chapter_id = source["end_chapter_id"];
	        this.content = source["content"];
	    }
	}
	export class ChunkRoundTrace {
	    round: number;
	    input_count: number;
	    output_count: number;
	    token_count: number;
	    batches: BatchTrace[];
	    chunks: Chunk[];
	
	    static createFrom(source: any = {}) {
	        return new ChunkRoundTrace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.round = source["round"];
	        this.input_count = source["input_count"];
	        this.output_count = source["output_count"];
	        this.token_count = source["token_count"];
	        this.batches = this.convertValues(source["batches"], BatchTrace);
	        this.chunks = this.convertValues(source["chunks"], Chunk);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Trace {
	    task_id?: string;
	    novel_id: number;
	    chapter_count: number;
	    context_window: number;
	    batch_budget: number;
	    boundaries: BoundaryHint[];
	    summaries: ChapterSummaryItem[];
	    chunk_rounds: ChunkRoundTrace[];
	    final_tokens: number;
	
	    static createFrom(source: any = {}) {
	        return new Trace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.novel_id = source["novel_id"];
	        this.chapter_count = source["chapter_count"];
	        this.context_window = source["context_window"];
	        this.batch_budget = source["batch_budget"];
	        this.boundaries = this.convertValues(source["boundaries"], BoundaryHint);
	        this.summaries = this.convertValues(source["summaries"], ChapterSummaryItem);
	        this.chunk_rounds = this.convertValues(source["chunk_rounds"], ChunkRoundTrace);
	        this.final_tokens = source["final_tokens"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FidelityScorecard {
	    known_consistency: number;
	    unknown_uncertainty: number;
	    issues?: string[];
	    passed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FidelityScorecard(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.known_consistency = source["known_consistency"];
	        this.unknown_uncertainty = source["unknown_uncertainty"];
	        this.issues = source["issues"];
	        this.passed = source["passed"];
	    }
	}
	export class ExtractPatternResult {
	    task_id?: string;
	    name: string;
	    description: string;
	    raw_content: string;
	    file_path: string;
	    flavor?: string;
	    fidelity?: FidelityScorecard;
	    demoted_models?: string[];
	    trace?: Trace;
	
	    static createFrom(source: any = {}) {
	        return new ExtractPatternResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.raw_content = source["raw_content"];
	        this.file_path = source["file_path"];
	        this.flavor = source["flavor"];
	        this.fidelity = this.convertValues(source["fidelity"], FidelityScorecard);
	        this.demoted_models = source["demoted_models"];
	        this.trace = this.convertValues(source["trace"], Trace);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExtractPatternAllResult {
	    task_id?: string;
	    struct?: ExtractPatternResult;
	    mental?: ExtractPatternResult;
	
	    static createFrom(source: any = {}) {
	        return new ExtractPatternAllResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.struct = this.convertValues(source["struct"], ExtractPatternResult);
	        this.mental = this.convertValues(source["mental"], ExtractPatternResult);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExtractPatternInput {
	    task_id?: string;
	    novel_id: number;
	    provider_name: string;
	    model_id: string;
	    reasoning_effort: string;
	    chapter_ids?: number[];
	    flavor?: string;
	    source_author?: string;
	    deep_multi_lens?: boolean;
	    flavors?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ExtractPatternInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.novel_id = source["novel_id"];
	        this.provider_name = source["provider_name"];
	        this.model_id = source["model_id"];
	        this.reasoning_effort = source["reasoning_effort"];
	        this.chapter_ids = source["chapter_ids"];
	        this.flavor = source["flavor"];
	        this.source_author = source["source_author"];
	        this.deep_multi_lens = source["deep_multi_lens"];
	        this.flavors = source["flavors"];
	    }
	}
	
	

}

export namespace personasetting {
	
	export class PersonaSetting {
	    id: number;
	    novel_id: number;
	    enabled: boolean;
	    kind: string;
	    ref: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new PersonaSetting(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.enabled = source["enabled"];
	        this.kind = source["kind"];
	        this.ref = source["ref"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace pipeline {
	
	export class DetectionTaskResult {
	    kind: string;
	    novel_id: number;
	    scope_id?: number;
	    run_id?: string;
	    dispatched: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DetectionTaskResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.novel_id = source["novel_id"];
	        this.scope_id = source["scope_id"];
	        this.run_id = source["run_id"];
	        this.dispatched = source["dispatched"];
	    }
	}
	export class DetectionTaskStatus {
	    kind: string;
	    novel_id: number;
	    scope_id?: number;
	    running: boolean;
	    last_run_id?: string;
	    last_verdict?: string;
	    last_issue_count: number;
	    // Go type: time
	    last_run_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new DetectionTaskStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.novel_id = source["novel_id"];
	        this.scope_id = source["scope_id"];
	        this.running = source["running"];
	        this.last_run_id = source["last_run_id"];
	        this.last_verdict = source["last_verdict"];
	        this.last_issue_count = source["last_issue_count"];
	        this.last_run_at = this.convertValues(source["last_run_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class IssueTicketRef {
	    run_id: string;
	    issue_key: string;
	
	    static createFrom(source: any = {}) {
	        return new IssueTicketRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run_id = source["run_id"];
	        this.issue_key = source["issue_key"];
	    }
	}
export class IssueVerifySummary {
    confirmed: number;
    rejected: number;
    unverified: number;
    queued: number;
    detail: string;

    static createFrom(source: any = {}) {
        return new IssueVerifySummary(source);
    }

    constructor(source: any = {}) {
        if ('string' === typeof source) source = JSON.parse(source);
        this.confirmed = source["confirmed"];
        this.rejected = source["rejected"];
        this.unverified = source["unverified"];
        this.queued = source["queued"];
        this.detail = source["detail"];
    }
}
	export class PatrolReportView {
	    report: string;
	    run_id: string;
	    source: string;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new PatrolReportView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.report = source["report"];
	        this.run_id = source["run_id"];
	        this.source = source["source"];
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PipelineIssue {
	    id: number;
	    novel_id: number;
	    run_id: string;
	    issue_key: string;
	    level: string;
	    area: string;
	    desc: string;
	    fix_type?: string;
	    target_scope?: string;
	    dimension?: string;
	    direction?: string;
	    chapter_ids?: string;
	    status: string;
	    skip_reason?: string;
	    verify_receipt?: string;
	    // Go type: time
	    verify_at?: any;
	    note?: string;
	    round: number;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new PipelineIssue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.run_id = source["run_id"];
	        this.issue_key = source["issue_key"];
	        this.level = source["level"];
	        this.area = source["area"];
	        this.desc = source["desc"];
	        this.fix_type = source["fix_type"];
	        this.target_scope = source["target_scope"];
	        this.dimension = source["dimension"];
	        this.direction = source["direction"];
	        this.chapter_ids = source["chapter_ids"];
	        this.status = source["status"];
	        this.skip_reason = source["skip_reason"];
	        this.verify_receipt = source["verify_receipt"];
	        this.verify_at = this.convertValues(source["verify_at"], null);
	        this.note = source["note"];
	        this.round = source["round"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PipelineRun {
	    ID: number;
	    NovelID: number;
	    RunID: string;
	    Mode: string;
	    StartChapter?: number;
	    CurrentStage: string;
	    Status: string;
	    AutoRun: boolean;
	    ContextJSON: string;
	    VolumeID?: number;
	    ParentRunID: string;
	    // Go type: time
	    CreatedAt: any;
	    // Go type: time
	    UpdatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new PipelineRun(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.NovelID = source["NovelID"];
	        this.RunID = source["RunID"];
	        this.Mode = source["Mode"];
	        this.StartChapter = source["StartChapter"];
	        this.CurrentStage = source["CurrentStage"];
	        this.Status = source["Status"];
	        this.AutoRun = source["AutoRun"];
	        this.ContextJSON = source["ContextJSON"];
	        this.VolumeID = source["VolumeID"];
	        this.ParentRunID = source["ParentRunID"];
	        this.CreatedAt = this.convertValues(source["CreatedAt"], null);
	        this.UpdatedAt = this.convertValues(source["UpdatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace preference {
	
	export class PreferenceItem {
	    id: number;
	    novel_id: number;
	    is_global: boolean;
	    category: string;
	    content: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new PreferenceItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.is_global = source["is_global"];
	        this.category = source["category"];
	        this.content = source["content"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace reader {
	
	export class ReaderPerspective {
	    id: number;
	    novel_id: number;
	    type: string;
	    content: string;
	    related_truth: string;
	    planted_chapter_id?: number;
	    revealed_chapter_id?: number;
	    // Go type: time
	    created_at: any;
	    volume_id?: number;
	    // Go type: time
	    changed_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new ReaderPerspective(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.type = source["type"];
	        this.content = source["content"];
	        this.related_truth = source["related_truth"];
	        this.planted_chapter_id = source["planted_chapter_id"];
	        this.revealed_chapter_id = source["revealed_chapter_id"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.volume_id = source["volume_id"];
	        this.changed_at = this.convertValues(source["changed_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace remote {
	
	export class RemoteSkillMeta {
	    name: string;
	    description: string;
	    category: string;
	    mode: string;
	    author: string;
	    version: number;
	    file: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteSkillMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.category = source["category"];
	        this.mode = source["mode"];
	        this.author = source["author"];
	        this.version = source["version"];
	        this.file = source["file"];
	    }
	}

}

export namespace retcon {
	
	export class CreateInput {
	    novel_id: number;
	    volume_id?: number;
	    origin?: string;
	    kind?: string;
	    issue_id?: string;
	    target_scope?: string;
	    dimension?: string;
	    direction?: string;
	    title?: string;
	    description?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.volume_id = source["volume_id"];
	        this.origin = source["origin"];
	        this.kind = source["kind"];
	        this.issue_id = source["issue_id"];
	        this.target_scope = source["target_scope"];
	        this.dimension = source["dimension"];
	        this.direction = source["direction"];
	        this.title = source["title"];
	        this.description = source["description"];
	    }
	}
	export class RetconCase {
	    id: number;
	    novel_id: number;
	    volume_id?: number;
	    origin: string;
	    kind: string;
	    issue_id?: string;
	    target_scope?: string;
	    dimension?: string;
	    direction?: string;
	    title: string;
	    description?: string;
	    status: string;
	    decision_note?: string;
	    decided_by?: string;
	    // Go type: time
	    decided_at?: any;
	    rollback_snapshot_id?: number;
	    diff_summary?: string;
	    applied_by?: string;
	    // Go type: time
	    applied_at?: any;
	    rolled_back_by?: string;
	    // Go type: time
	    rolled_back_at?: any;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new RetconCase(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.volume_id = source["volume_id"];
	        this.origin = source["origin"];
	        this.kind = source["kind"];
	        this.issue_id = source["issue_id"];
	        this.target_scope = source["target_scope"];
	        this.dimension = source["dimension"];
	        this.direction = source["direction"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.status = source["status"];
	        this.decision_note = source["decision_note"];
	        this.decided_by = source["decided_by"];
	        this.decided_at = this.convertValues(source["decided_at"], null);
	        this.rollback_snapshot_id = source["rollback_snapshot_id"];
	        this.diff_summary = source["diff_summary"];
	        this.applied_by = source["applied_by"];
	        this.applied_at = this.convertValues(source["applied_at"], null);
	        this.rolled_back_by = source["rolled_back_by"];
	        this.rolled_back_at = this.convertValues(source["rolled_back_at"], null);
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace search {
	
	export class Result {
	    type: string;
	    id: number;
	    title: string;
	    subtitle: string;
	    chapter_id: number;
	    reading_number: number;
	    file_path: string;
	    match_prefix: string;
	    match_hit: string;
	    match_suffix: string;
	    match_position: number;
	    match_len: number;
	    relevance: number;
	    panel_id: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.id = source["id"];
	        this.title = source["title"];
	        this.subtitle = source["subtitle"];
	        this.chapter_id = source["chapter_id"];
	        this.reading_number = source["reading_number"];
	        this.file_path = source["file_path"];
	        this.match_prefix = source["match_prefix"];
	        this.match_hit = source["match_hit"];
	        this.match_suffix = source["match_suffix"];
	        this.match_position = source["match_position"];
	        this.match_len = source["match_len"];
	        this.relevance = source["relevance"];
	        this.panel_id = source["panel_id"];
	    }
	}

}

export namespace session {
	
	export class Message {
	    id: number;
	    session_id: string;
	    turn_id: number;
	    role: string;
	    content: string;
	    thinking_content?: string;
	    token_count: number;
	    extra_metadata?: string;
	    version: number;
	    to_api: boolean;
	    to_frontend: boolean;
	    event_type?: string;
	    agent_type: string;
	    sub_task_id?: string;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Message(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.turn_id = source["turn_id"];
	        this.role = source["role"];
	        this.content = source["content"];
	        this.thinking_content = source["thinking_content"];
	        this.token_count = source["token_count"];
	        this.extra_metadata = source["extra_metadata"];
	        this.version = source["version"];
	        this.to_api = source["to_api"];
	        this.to_frontend = source["to_frontend"];
	        this.event_type = source["event_type"];
	        this.agent_type = source["agent_type"];
	        this.sub_task_id = source["sub_task_id"];
	        this.created_at = this.convertValues(source["created_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace setting {
	
	export class SettingItem {
	    id: number;
	    novel_id: number;
	    category: string;
	    content: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new SettingItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.category = source["category"];
	        this.content = source["content"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace skill {
	
	export class UseWhenEntry {
	    intent: string;
	    cues: string[];
	
	    static createFrom(source: any = {}) {
	        return new UseWhenEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.intent = source["intent"];
	        this.cues = source["cues"];
	    }
	}
	export class Skill {
	    name: string;
	    description: string;
	    category: string;
	    kind: string;
	    mode: string;
	    use_when?: UseWhenEntry[];
	    author: string;
	    source_author: string;
	    version: number;
	    content: string;
	    raw_content?: string;
	    aggregated: boolean;
	    aggregated_from: string[];
	    merged_books: string[];
	    stable_items: number;
	    single_book_items: number;
	    aggregated_at: string;
	    aggregated_source_hash: string;
	
	    static createFrom(source: any = {}) {
	        return new Skill(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.category = source["category"];
	        this.kind = source["kind"];
	        this.mode = source["mode"];
	        this.use_when = this.convertValues(source["use_when"], UseWhenEntry);
	        this.author = source["author"];
	        this.source_author = source["source_author"];
	        this.version = source["version"];
	        this.content = source["content"];
	        this.raw_content = source["raw_content"];
	        this.aggregated = source["aggregated"];
	        this.aggregated_from = source["aggregated_from"];
	        this.merged_books = source["merged_books"];
	        this.stable_items = source["stable_items"];
	        this.single_book_items = source["single_book_items"];
	        this.aggregated_at = source["aggregated_at"];
	        this.aggregated_source_hash = source["aggregated_source_hash"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SkillMeta {
	    name: string;
	    description: string;
	    category: string;
	    kind: string;
	    mode: string;
	    use_when?: UseWhenEntry[];
	    author: string;
	    source_author: string;
	    version: number;
	    source: string;
	    aggregated: boolean;
	    aggregated_from: string[];
	    merged_books: string[];
	    stable_items: number;
	    single_book_items: number;
	
	    static createFrom(source: any = {}) {
	        return new SkillMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.category = source["category"];
	        this.kind = source["kind"];
	        this.mode = source["mode"];
	        this.use_when = this.convertValues(source["use_when"], UseWhenEntry);
	        this.author = source["author"];
	        this.source_author = source["source_author"];
	        this.version = source["version"];
	        this.source = source["source"];
	        this.aggregated = source["aggregated"];
	        this.aggregated_from = source["aggregated_from"];
	        this.merged_books = source["merged_books"];
	        this.stable_items = source["stable_items"];
	        this.single_book_items = source["single_book_items"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace snapshot {
	
	export class AuditIssue {
	    severity: string;
	    table: string;
	    row_id: number;
	    code: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new AuditIssue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.severity = source["severity"];
	        this.table = source["table"];
	        this.row_id = source["row_id"];
	        this.code = source["code"];
	        this.message = source["message"];
	    }
	}
	export class AuditReport {
	    novel_id: number;
	    issues: AuditIssue[];
	    error_count: number;
	    warning_count: number;
	
	    static createFrom(source: any = {}) {
	        return new AuditReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.issues = this.convertValues(source["issues"], AuditIssue);
	        this.error_count = source["error_count"];
	        this.warning_count = source["warning_count"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FieldDelta {
	    old: any;
	    new: any;
	
	    static createFrom(source: any = {}) {
	        return new FieldDelta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.old = source["old"];
	        this.new = source["new"];
	    }
	}
	export class RowDelta {
	    id: number;
	    fields: Record<string, FieldDelta>;
	
	    static createFrom(source: any = {}) {
	        return new RowDelta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.fields = this.convertValues(source["fields"], FieldDelta, true);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TableDelta {
	    table: string;
	    added?: any[];
	    removed?: any[];
	    changed?: RowDelta[];
	
	    static createFrom(source: any = {}) {
	        return new TableDelta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.table = source["table"];
	        this.added = source["added"];
	        this.removed = source["removed"];
	        this.changed = this.convertValues(source["changed"], RowDelta);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Delta {
	    schema_version: number;
	    base_version: number;
	    target_version: number;
	    tables: TableDelta[];
	
	    static createFrom(source: any = {}) {
	        return new Delta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema_version = source["schema_version"];
	        this.base_version = source["base_version"];
	        this.target_version = source["target_version"];
	        this.tables = this.convertValues(source["tables"], TableDelta);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	

}

export namespace storage {
	
	export class PageResult_github_com_novelgo_novelgo_app_SessionMeta_ {
	    items: app.SessionMeta[];
	    total: number;
	    page: number;
	    size: number;
	    total_pages: number;
	
	    static createFrom(source: any = {}) {
	        return new PageResult_github_com_novelgo_novelgo_app_SessionMeta_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], app.SessionMeta);
	        this.total = source["total"];
	        this.page = source["page"];
	        this.size = source["size"];
	        this.total_pages = source["total_pages"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PageResult_github_com_novelgo_novelgo_internal_character_GroupMember_ {
	    items: character.GroupMember[];
	    total: number;
	    page: number;
	    size: number;
	    total_pages: number;
	
	    static createFrom(source: any = {}) {
	        return new PageResult_github_com_novelgo_novelgo_internal_character_GroupMember_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], character.GroupMember);
	        this.total = source["total"];
	        this.page = source["page"];
	        this.size = source["size"];
	        this.total_pages = source["total_pages"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PageResult_github_com_novelgo_novelgo_internal_character_GroupView_ {
	    items: character.GroupView[];
	    total: number;
	    page: number;
	    size: number;
	    total_pages: number;
	
	    static createFrom(source: any = {}) {
	        return new PageResult_github_com_novelgo_novelgo_internal_character_GroupView_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], character.GroupView);
	        this.total = source["total"];
	        this.page = source["page"];
	        this.size = source["size"];
	        this.total_pages = source["total_pages"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PageResult_github_com_novelgo_novelgo_internal_skill_remote_RemoteSkillMeta_ {
	    items: remote.RemoteSkillMeta[];
	    total: number;
	    page: number;
	    size: number;
	    total_pages: number;
	
	    static createFrom(source: any = {}) {
	        return new PageResult_github_com_novelgo_novelgo_internal_skill_remote_RemoteSkillMeta_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], remote.RemoteSkillMeta);
	        this.total = source["total"];
	        this.page = source["page"];
	        this.size = source["size"];
	        this.total_pages = source["total_pages"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class PageResult_github_com_novelgo_novelgo_internal_style_Sample_ {
	    items: style.Sample[];
	    total: number;
	    page: number;
	    size: number;
	    total_pages: number;
	
	    static createFrom(source: any = {}) {
	        return new PageResult_github_com_novelgo_novelgo_internal_style_Sample_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], style.Sample);
	        this.total = source["total"];
	        this.page = source["page"];
	        this.size = source["size"];
	        this.total_pages = source["total_pages"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace storyarc {
	
	export class ArcNode {
	    id: number;
	    novel_id: number;
	    story_arc_id: number;
	    title: string;
	    description: string;
	    target_reading_number: number;
	    actual_chapter_id?: number;
	    status: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    volume_id?: number;
	    // Go type: time
	    changed_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new ArcNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.story_arc_id = source["story_arc_id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.target_reading_number = source["target_reading_number"];
	        this.actual_chapter_id = source["actual_chapter_id"];
	        this.status = source["status"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.volume_id = source["volume_id"];
	        this.changed_at = this.convertValues(source["changed_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class StoryArc {
	    id: number;
	    novel_id: number;
	    name: string;
	    description: string;
	    arc_type: string;
	    importance: number;
	    status: string;
	    reactivate_at: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    char_id?: number;
	    motif: string;
	    volume_id?: number;
	    // Go type: time
	    changed_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new StoryArc(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.arc_type = source["arc_type"];
	        this.importance = source["importance"];
	        this.status = source["status"];
	        this.reactivate_at = source["reactivate_at"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.char_id = source["char_id"];
	        this.motif = source["motif"];
	        this.volume_id = source["volume_id"];
	        this.changed_at = this.convertValues(source["changed_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace style {
	
	export class ExtractResult {
	    name: string;
	    description: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new ExtractResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.content = source["content"];
	    }
	}
	export class Profile {
	    id: number;
	    novel_id: number;
	    is_global: boolean;
	    name: string;
	    description: string;
	    content: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Profile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.is_global = source["is_global"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.content = source["content"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Sample {
	    id: number;
	    novel_id: number;
	    is_global: boolean;
	    name: string;
	    content: string;
	    preview: string;
	    tags: string[];
	    word_count: number;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Sample(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.is_global = source["is_global"];
	        this.name = source["name"];
	        this.content = source["content"];
	        this.preview = source["preview"];
	        this.tags = source["tags"];
	        this.word_count = source["word_count"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Stats {
	    total_chars: number;
	    total_words: number;
	    sentence_count: number;
	    short_sent_pct: number;
	    mid_sent_pct: number;
	    long_sent_pct: number;
	    avg_sent_len: number;
	    sent_len_std_dev: number;
	    comma_density: number;
	    period_density: number;
	    exclaim_density: number;
	    question_density: number;
	    quote_density: number;
	    paragraph_count: number;
	    avg_para_len: number;
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total_chars = source["total_chars"];
	        this.total_words = source["total_words"];
	        this.sentence_count = source["sentence_count"];
	        this.short_sent_pct = source["short_sent_pct"];
	        this.mid_sent_pct = source["mid_sent_pct"];
	        this.long_sent_pct = source["long_sent_pct"];
	        this.avg_sent_len = source["avg_sent_len"];
	        this.sent_len_std_dev = source["sent_len_std_dev"];
	        this.comma_density = source["comma_density"];
	        this.period_density = source["period_density"];
	        this.exclaim_density = source["exclaim_density"];
	        this.question_density = source["question_density"];
	        this.quote_density = source["quote_density"];
	        this.paragraph_count = source["paragraph_count"];
	        this.avg_para_len = source["avg_para_len"];
	    }
	}

}

export namespace timeline {
	
	export class ChapterPlan {
	    novel_id: number;
	    scope: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new ChapterPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.scope = source["scope"];
	        this.content = source["content"];
	    }
	}
	export class TimelineEntry {
	    id: number;
	    novel_id: number;
	    category: string;
	    status: string;
	    title: string;
	    content: string;
	    detail_json: string;
	    target_reading_number: number;
	    importance: number;
	    source_chapter_id?: number;
	    source: string;
	    resolved_chapter_id?: number;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    volume_id?: number;
	    // Go type: time
	    changed_at?: any;
	
	    static createFrom(source: any = {}) {
	        return new TimelineEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.category = source["category"];
	        this.status = source["status"];
	        this.title = source["title"];
	        this.content = source["content"];
	        this.detail_json = source["detail_json"];
	        this.target_reading_number = source["target_reading_number"];
	        this.importance = source["importance"];
	        this.source_chapter_id = source["source_chapter_id"];
	        this.source = source["source"];
	        this.resolved_chapter_id = source["resolved_chapter_id"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.volume_id = source["volume_id"];
	        this.changed_at = this.convertValues(source["changed_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace update {
	
	export class ReleaseInfo {
	    tag_name: string;
	    name: string;
	    body: string;
	    html_url: string;
	    published_at: string;
	
	    static createFrom(source: any = {}) {
	        return new ReleaseInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tag_name = source["tag_name"];
	        this.name = source["name"];
	        this.body = source["body"];
	        this.html_url = source["html_url"];
	        this.published_at = source["published_at"];
	    }
	}
	export class CheckResult {
	    currentVersion: string;
	    latest: ReleaseInfo;
	    hasUpdate: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.currentVersion = source["currentVersion"];
	        this.latest = this.convertValues(source["latest"], ReleaseInfo);
	        this.hasUpdate = source["hasUpdate"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace voice {
	
	export class Entry {
	    id: number;
	    novel_id: number;
	    dimension: string;
	    content: string;
	    metrics: string;
	    evidence: string;
	    source: string;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.dimension = source["dimension"];
	        this.content = source["content"];
	        this.metrics = source["metrics"];
	        this.evidence = source["evidence"];
	        this.source = source["source"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace volume {
	
	export class PlaceInput {
	    novel_id: number;
	    source_volume_id?: number;
	    name?: string;
	    before_volume_id?: number;
	
	    static createFrom(source: any = {}) {
	        return new PlaceInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.source_volume_id = source["source_volume_id"];
	        this.name = source["name"];
	        this.before_volume_id = source["before_volume_id"];
	    }
	}
	export class Volume {
	    id: number;
	    novel_id: number;
	    name: string;
	    sort_order: number;
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	    outline_file_path: string;
	    frozen: boolean;
	    // Go type: time
	    frozen_at?: any;
	    snapshot_version: number;
	
	    static createFrom(source: any = {}) {
	        return new Volume(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.novel_id = source["novel_id"];
	        this.name = source["name"];
	        this.sort_order = source["sort_order"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	        this.outline_file_path = source["outline_file_path"];
	        this.frozen = source["frozen"];
	        this.frozen_at = this.convertValues(source["frozen_at"], null);
	        this.snapshot_version = source["snapshot_version"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace writing {
	
	export class DailyActivity {
	    date: string;
	    words_net: number;
	    words_added: number;
	    words_deleted: number;
	
	    static createFrom(source: any = {}) {
	        return new DailyActivity(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.date = source["date"];
	        this.words_net = source["words_net"];
	        this.words_added = source["words_added"];
	        this.words_deleted = source["words_deleted"];
	    }
	}
	export class WritingStats {
	    total_words: number;
	    total_days_active: number;
	    current_streak: number;
	    longest_streak: number;
	    total_novels: number;
	    total_chapters: number;
	
	    static createFrom(source: any = {}) {
	        return new WritingStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total_words = source["total_words"];
	        this.total_days_active = source["total_days_active"];
	        this.current_streak = source["current_streak"];
	        this.longest_streak = source["longest_streak"];
	        this.total_novels = source["total_novels"];
	        this.total_chapters = source["total_chapters"];
	    }
	}

}

export namespace writingconfig {
	
	export class WritingConfig {
	    novel_id: number;
	    auto_score_enabled: boolean;
	    score_threshold: number;
	    score_max_retries: number;
	
	    static createFrom(source: any = {}) {
	        return new WritingConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.novel_id = source["novel_id"];
	        this.auto_score_enabled = source["auto_score_enabled"];
	        this.score_threshold = source["score_threshold"];
	        this.score_max_retries = source["score_max_retries"];
	    }
	}

}

