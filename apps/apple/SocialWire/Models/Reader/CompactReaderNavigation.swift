import Foundation

/// Pure navigation decisions for the compact horizontal pager.
enum CompactReaderNavigation {
    /// Pane shown after choosing a list source on the Lists pane.
    static func paneAfterListSource(_: ReaderListSource) -> ReaderPane {
        .publications
    }

    /// Pane shown after choosing a publication.
    static func paneAfterPublication(_ source: ReaderListSource) -> ReaderPane {
        source.compactUsesArticlesPane ? .articles : .publications
    }

    /// Pane shown after opening an article or saved link.
    static func paneAfterDetail() -> ReaderPane {
        .reader
    }

    /// Side effects when swiping between compact panes.
    struct SwipeTransition: Equatable {
        let clearsReaderDetail: Bool
        let clearsArticleSelection: Bool
        let clearsFeedState: Bool
    }

    static func swipeTransition(
        from oldPane: ReaderPane,
        to newPane: ReaderPane,
        usesArticlesPane: Bool
    ) -> SwipeTransition {
        var clearsReaderDetail = false
        var clearsArticleSelection = false
        var clearsFeedState = false

        if usesArticlesPane {
            if newPane == .articles, oldPane == .reader {
                clearsReaderDetail = true
            }
            if newPane == .publications, oldPane == .articles {
                clearsArticleSelection = true
            }
        }

        if newPane == .lists, oldPane != .lists {
            clearsFeedState = true
        }

        return SwipeTransition(
            clearsReaderDetail: clearsReaderDetail,
            clearsArticleSelection: clearsArticleSelection,
            clearsFeedState: clearsFeedState
        )
    }

    /// Remap compact pane when switching to a three-pane list source.
    static func remapPaneAfterListSourceChange(
        compactPane: ReaderPane,
        newSource: ReaderListSource
    ) -> ReaderPane? {
        guard !newSource.compactUsesArticlesPane, compactPane == .articles else { return nil }
        return .publications
    }

    /// Whether a programmatic detail selection should advance the compact pager.
    static func shouldAdvanceToReader(
        compactPane: ReaderPane,
        hasDetailSelection: Bool
    ) -> Bool {
        hasDetailSelection && compactPane != .reader
    }
}
