import { ServerModelStatus } from '$lib/enums';
import { apiFetch, apiPost } from '$lib/utils';
import type { ParsedModelId } from '$lib/types/models';
import {
	MODEL_QUANTIZATION_SEGMENT_RE,
	MODEL_CUSTOM_QUANTIZATION_PREFIX_RE,
	MODEL_PARAMS_RE,
	MODEL_ACTIVATED_PARAMS_RE,
	MODEL_IGNORED_SEGMENTS,
	MODEL_ID_NOT_FOUND,
	MODEL_ID_ORG_SEPARATOR,
	MODEL_ID_SEGMENT_SEPARATOR,
	MODEL_ID_QUANTIZATION_SEPARATOR
} from '$lib/constants';

export interface ModelConfig {
	Name: string;
	APIType: string;
	BaseURL: string;
	APIKey: string;
	Model: string;
	Temperature: number;
	MaxTokens: number;
	Stream: boolean;
	Thinking: boolean;
	BlockDangerousCommands: boolean;
	Description: string;
}

export interface ModelsListResponse {
	Models: ModelConfig[];
	MainModel: string;
}

export class ModelsService {
	/**
	 *
	 *
	 * Listing
	 *
	 *
	 */

	/**
	 * Fetch list of models from GhostClaw API endpoint.
	 *
	 * @returns List of available models with configuration
	 */
	static async list(): Promise<ModelsListResponse> {
		return apiFetch<ModelsListResponse>('/api/models');
	}

	/**
	 * Create a new model
	 *
	 * @param model - Model configuration
	 * @returns Created model configuration
	 */
	static async create(model: Omit<ModelConfig, 'Name'> & { Name: string }): Promise<ModelConfig> {
		return apiPost<ModelConfig>('/api/models', model);
	}

	/**
	 * Get model details
	 *
	 * @param name - Model name
	 * @returns Model configuration
	 */
	static async get(name: string): Promise<ModelConfig> {
		return apiFetch<ModelConfig>(`/api/models/${name}`);
	}

	/**
	 * Update model configuration
	 *
	 * @param name - Model name
	 * @param model - Model configuration updates
	 * @returns Updated model configuration
	 */
	static async update(name: string, model: Partial<ModelConfig>): Promise<ModelConfig> {
		return apiPost<ModelConfig>(`/api/models/${name}`, model, {
			method: 'PUT'
		});
	}

	/**
	 * Delete a model
	 *
	 * @param name - Model name
	 * @returns Success response
	 */
	static async delete(name: string): Promise<{ message: string }> {
		return apiPost<{ message: string }>(`/api/models/${name}`, {}, {
			method: 'DELETE'
		});
	}

	/**
	 * Set model as main model
	 *
	 * @param name - Model name
	 * @returns Success response
	 */
	static async setMain(name: string): Promise<{ message: string }> {
		return apiPost<{ message: string }>(`/api/models/${name}/set-main`, {}, {
			method: 'PATCH'
		});
	}

	/**
	 *
	 *
	 * Status
	 *
	 *
	 */

	/**
	 * Check if a model is loaded based on its metadata.
	 *
	 * @param model - Model data entry from the API response
	 * @returns True if the model status is LOADED
	 */
	static isModelLoaded(model: ApiModelDataEntry): boolean {
		return model.status.value === ServerModelStatus.LOADED;
	}

	/**
	 * Check if a model is currently loading.
	 *
	 * @param model - Model data entry from the API response
	 * @returns True if the model status is LOADING
	 */
	static isModelLoading(model: ApiModelDataEntry): boolean {
		return model.status.value === ServerModelStatus.LOADING;
	}

	/**
	 *
	 *
	 * Parsing
	 *
	 *
	 */

	/**
	 * Parse a model ID string into its structured components.
	 *
	 * Handles conventions like:
	 *   `<org>/<ModelName>-<Parameters>(-<ActivatedParameters>)(-<Tags>)(-<Quantization>):<Quantization>`
	 *   `<ModelName>.<Quantization>` (dot-separated quantization, e.g. `model.Q4_K_M`)
	 *
	 * @param modelId - Raw model identifier string
	 * @returns Structured {@link ParsedModelId} with all detected fields
	 */
	static parseModelId(modelId: string): ParsedModelId {
		const result: ParsedModelId = {
			raw: modelId,
			orgName: null,
			modelName: null,
			params: null,
			activatedParams: null,
			quantization: null,
			tags: []
		};

		// 1. Extract colon-separated quantization (e.g. `model:Q4_K_M`)
		const colonIdx = modelId.indexOf(MODEL_ID_QUANTIZATION_SEPARATOR);
		let modelPath: string;

		if (colonIdx !== MODEL_ID_NOT_FOUND) {
			result.quantization = modelId.slice(colonIdx + 1) || null;
			modelPath = modelId.slice(0, colonIdx);
		} else {
			modelPath = modelId;
		}

		// 2. Extract org name (e.g. `org/model` -> org = "org")
		const slashIdx = modelPath.indexOf(MODEL_ID_ORG_SEPARATOR);
		let modelStr: string;

		if (slashIdx !== MODEL_ID_NOT_FOUND) {
			result.orgName = modelPath.slice(0, slashIdx);
			modelStr = modelPath.slice(slashIdx + 1);
		} else {
			modelStr = modelPath;
		}

		// 3. Handle dot-separated quantization (e.g. `model-name.Q4_K_M`)
		const dotIdx = modelStr.lastIndexOf('.');

		if (dotIdx !== MODEL_ID_NOT_FOUND && !result.quantization) {
			const afterDot = modelStr.slice(dotIdx + 1);

			if (MODEL_QUANTIZATION_SEGMENT_RE.test(afterDot)) {
				result.quantization = afterDot;
				modelStr = modelStr.slice(0, dotIdx);
			}
		}

		const segments = modelStr.split(MODEL_ID_SEGMENT_SEPARATOR);

		// 4. Detect trailing quantization from dash-separated segments
		//    Handle UD-prefixed quantization (e.g. `UD-Q8_K_XL`) and
		//    standalone quantization (e.g. `Q4_K_M`, `BF16`, `F16`, `MXFP4`)
		if (!result.quantization && segments.length > 1) {
			const last = segments[segments.length - 1];
			const secondLast = segments.length > 2 ? segments[segments.length - 2] : null;

			if (MODEL_QUANTIZATION_SEGMENT_RE.test(last)) {
				if (secondLast && MODEL_CUSTOM_QUANTIZATION_PREFIX_RE.test(secondLast)) {
					result.quantization = `${secondLast}-${last}`;
					segments.splice(segments.length - 2, 2);
				} else {
					result.quantization = last;
					segments.pop();
				}
			}
		}

		// 5. Find params and activated params
		let paramsIdx = MODEL_ID_NOT_FOUND;
		let activatedParamsIdx = MODEL_ID_NOT_FOUND;

		for (let i = 0; i < segments.length; i++) {
			const seg = segments[i];

			if (paramsIdx === MODEL_ID_NOT_FOUND && MODEL_PARAMS_RE.test(seg)) {
				paramsIdx = i;
				result.params = seg.toUpperCase();
			} else if (paramsIdx !== MODEL_ID_NOT_FOUND && MODEL_ACTIVATED_PARAMS_RE.test(seg)) {
				activatedParamsIdx = i;
				result.activatedParams = seg.toUpperCase();
			}
		}

		// 6. Model name = segments before params; tags = remaining segments after params
		const pivotIdx = paramsIdx !== MODEL_ID_NOT_FOUND ? paramsIdx : segments.length;

		result.modelName = segments.slice(0, pivotIdx).join(MODEL_ID_SEGMENT_SEPARATOR) || null;

		if (paramsIdx !== MODEL_ID_NOT_FOUND) {
			result.tags = segments.slice(paramsIdx + 1).filter((_, relIdx) => {
				const absIdx = paramsIdx + 1 + relIdx;
				if (absIdx === activatedParamsIdx) return false;

				return !MODEL_IGNORED_SEGMENTS.has(segments[absIdx].toUpperCase());
			});
		}

		return result;
	}
}
