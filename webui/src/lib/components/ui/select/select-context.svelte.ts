import { getContext, setContext } from 'svelte';
import { untrack } from 'svelte';

export const SELECT_CONTEXT_KEY = Symbol('select-context');

export interface SelectContext {
	selectedValue: string | undefined;
	selectedLabel: string | undefined;
	setSelectedValue: (value: string | undefined) => void;
	setSelectedLabel: (label: string | undefined) => void;
}

export function createSelectContext() {
	let selectedValue = $state<string | undefined>(undefined);
	let selectedLabel = $state<string | undefined>(undefined);

	const context: SelectContext = {
		get selectedValue() {
			return selectedValue;
		},
		get selectedLabel() {
			return selectedLabel;
		},
		setSelectedValue: (value) => {
			selectedValue = value;
		},
		setSelectedLabel: (label) => {
			selectedLabel = label;
		}
	};

	setContext(SELECT_CONTEXT_KEY, context);
	return context;
}

export function useSelectContext(): SelectContext | undefined {
	return getContext<SelectContext>(SELECT_CONTEXT_KEY);
}
