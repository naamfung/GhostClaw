import { AUTO_SCROLL_AT_BOTTOM_THRESHOLD, AUTO_SCROLL_INTERVAL } from '$lib/constants';

export interface AutoScrollOptions {
	/** Whether auto-scroll is disabled globally (e.g., from settings) */
	disabled?: boolean;
}

/**
 * Creates an auto-scroll controller for a scrollable container.
 *
 * Features:
 * - Auto-scrolls to bottom during streaming/loading
 * - Stops auto-scroll when user manually scrolls up
 * - Resumes auto-scroll when user scrolls back to bottom
 */
export class AutoScrollController {
	private _autoScrollEnabled = $state(true);
	private _userScrolledUp = $state(false);
	private _lastScrollTop = $state(0);
	private _scrollInterval: ReturnType<typeof setInterval> | undefined;
	private _scrollTimeout: ReturnType<typeof setTimeout> | undefined;
	private _container: HTMLElement | undefined;
	private _disabled: boolean;
	private _isProgrammaticScroll = false;

	constructor(options: AutoScrollOptions = {}) {
		this._disabled = options.disabled ?? false;
	}

	get autoScrollEnabled(): boolean {
		return this._autoScrollEnabled;
	}

	get userScrolledUp(): boolean {
		return this._userScrolledUp;
	}

	/**
	 * Binds the controller to a scrollable container element.
	 */
	setContainer(container: HTMLElement | undefined): void {
		this._container = container;
	}

	/**
	 * Updates the disabled state.
	 */
	setDisabled(disabled: boolean): void {
		this._disabled = disabled;
		if (disabled) {
			this._autoScrollEnabled = false;
			this.stopInterval();
		}
	}

	/**
	 * Handles scroll events to detect user scroll direction and toggle auto-scroll.
	 */
	handleScroll(): void {
		if (this._disabled || !this._container) return;

		// 跳過程式化滾動事件（由 scrollToBottom 觸發），避免干擾用戶滾動檢測
		if (this._isProgrammaticScroll) return;

		const { scrollTop, scrollHeight, clientHeight } = this._container;
		const distanceFromBottom = scrollHeight - scrollTop - clientHeight;
		const isAtBottom = distanceFromBottom < AUTO_SCROLL_AT_BOTTOM_THRESHOLD;

		if (scrollTop < this._lastScrollTop && !isAtBottom) {
			this._userScrolledUp = true;
			this._autoScrollEnabled = false;
		} else if (isAtBottom && this._userScrolledUp) {
			this._userScrolledUp = false;
			this._autoScrollEnabled = true;
		}

		// 延遲重啟：若用戶短暫向上滾動後立刻回到底部，自動恢復自動滾動
		if (!this._scrollTimeout) {
			this._scrollTimeout = setTimeout(() => {
				this._scrollTimeout = undefined;
				if (isAtBottom) {
					this._userScrolledUp = false;
					this._autoScrollEnabled = true;
				}
			}, AUTO_SCROLL_INTERVAL);
		}

		this._lastScrollTop = scrollTop;
	}

	/**
	 * Scrolls the container to the bottom.
	 * _isProgrammaticScroll flag 會跳過程式化 scroll events，所以 smooth
	 * 動畫期間嘅中間 scroll events 唔會觸發「用戶向上滾動」誤判。
	 */
	scrollToBottom(behavior: ScrollBehavior = 'smooth'): void {
		if (this._disabled || !this._container) return;

		this._isProgrammaticScroll = true;
		this._container.scrollTo({
			top: this._container.scrollHeight,
			behavior
		});
		// 用 requestAnimationFrame 確保 scroll event 已被觸發並跳過
		requestAnimationFrame(() => {
			this._isProgrammaticScroll = false;
		});
	}

	/**
	 * Enables auto-scroll (e.g., when user sends a message).
	 */
	enable(): void {
		if (this._disabled) return;
		this._userScrolledUp = false;
		this._autoScrollEnabled = true;
	}

	/**
	 * Starts the auto-scroll interval for continuous scrolling during streaming.
	 */
	startInterval(): void {
		if (this._disabled || this._scrollInterval) return;

		this._scrollInterval = setInterval(() => {
			this.scrollToBottom();
		}, AUTO_SCROLL_INTERVAL);
	}

	/**
	 * Stops the auto-scroll interval.
	 */
	stopInterval(): void {
		if (this._scrollInterval) {
			clearInterval(this._scrollInterval);
			this._scrollInterval = undefined;
		}
	}

	/**
	 * Updates the auto-scroll interval based on streaming state.
	 * Call this in a $effect to automatically manage the interval.
	 */
	updateInterval(isStreaming: boolean): void {
		if (this._disabled) {
			this.stopInterval();
			return;
		}

		if (isStreaming && this._autoScrollEnabled) {
			if (!this._scrollInterval) {
				this.startInterval();
			}
		} else {
			this.stopInterval();
		}
	}

	/**
	 * Cleans up resources. Call this in onDestroy or when the component unmounts.
	 */
	destroy(): void {
		this.stopInterval();
		if (this._scrollTimeout) {
			clearTimeout(this._scrollTimeout);
			this._scrollTimeout = undefined;
		}
	}
}

/**
 * Creates a new AutoScrollController instance.
 */
export function createAutoScrollController(options: AutoScrollOptions = {}): AutoScrollController {
	return new AutoScrollController(options);
}
