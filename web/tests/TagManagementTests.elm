module TagManagementTests exposing (allPassed, suite)

import Domain.TagManagement as Domain exposing (Mode(..), Preview, Receipt)
import Feature.TagManagement as TM
import Http
import Json.Decode as Decode


ctx : TM.Context
ctx =
    TM.context "" (Just "tok") True 7


step : TM.Msg -> TM.Model -> ( TM.Model, List TM.OutMsg )
step msg model =
    let
        ( m, _, outs ) =
            TM.update ctx msg model
    in
    ( m, outs )


preview : Preview
preview =
    { version = 3, mode = Rename, sourceTag = "old", targetTag = "new", affectedPostCount = 5, visiblePostCount = 2, targetPostCount = 0 }


receipt : Receipt
receipt =
    { id = "1", version = 4, mode = Rename, sourceTag = "old", targetTag = "new", affectedPostCount = 5, visiblePostCount = 2, changed = True }


draft : { mode : Mode, source : String, target : String }
draft =
    { mode = Rename, source = "old", target = "new" }


ready : TM.Model
ready =
    TM.init
        |> step (TM.SourceChosen "old") |> Tuple.first
        |> step (TM.TargetChanged "new") |> Tuple.first
        |> step TM.PreviewRequested |> Tuple.first
        |> step (TM.PreviewCompleted 7 3 draft (Ok preview)) |> Tuple.first


applying : TM.Model
applying =
    ready |> step TM.ApplyRequested |> Tuple.first


openedWithRows : TM.Model
openedWithRows =
    TM.init
        |> step TM.OpenRequested |> Tuple.first
        |> step (TM.ListLoaded 7 1 "" (Ok { version = 3, tags = [ { tag = "old", postCount = 5, visiblePostCount = 2 }, { tag = "other", postCount = 1, visiblePostCount = 1 } ], nextCursor = Just "c1" })) |> Tuple.first


caseEditingInvalidates : Bool
caseEditingInvalidates =
    let
        ( m, _ ) =
            step (TM.TargetChanged "other") ready
    in
    TM.currentPreview m == Nothing && not (TM.canConfirm m)


caseEditingSourceInvalidates : Bool
caseEditingSourceInvalidates =
    let
        ( m, _ ) =
            step (TM.SourceChanged "x") ready
    in
    TM.currentPreview m == Nothing && not (TM.canConfirm m)


caseModeInvalidates : Bool
caseModeInvalidates =
    let
        ( m, _ ) =
            step (TM.ModeChanged Merge) ready
    in
    TM.currentPreview m == Nothing && not (TM.canConfirm m)


casePrefixInvalidates : Bool
casePrefixInvalidates =
    let
        ( m, _ ) =
            step (TM.PrefixChanged "o") ready
    in
    TM.currentPreview m == Nothing && not (TM.canConfirm m)


caseLatePreviewIgnored : Bool
caseLatePreviewIgnored =
    let
        edited =
            ready |> step (TM.TargetChanged "other") |> Tuple.first

        ( m, outs ) =
            step (TM.PreviewCompleted 7 1 draft (Ok preview)) edited
    in
    outs == [] && TM.currentPreview m == Nothing && m == edited


caseLateListIgnored : Bool
caseLateListIgnored =
    let
        ( m, outs ) =
            step (TM.ListLoaded 7 1 "zzz" (Ok { version = 9, tags = [ { tag = "x", postCount = 1, visiblePostCount = 1 } ], nextCursor = Nothing })) openedWithRows
    in
    outs == [] && List.map .tag (TM.rows m) == [ "old", "other" ]


caseLateReceiptGenerationIgnored : Bool
caseLateReceiptGenerationIgnored =
    let
        ( m, outs ) =
            step (TM.ApplyCompleted 3 1 draft preview (Ok receipt)) applying
    in
    outs == [] && m == applying


caseAccessGenerationListIgnored : Bool
caseAccessGenerationListIgnored =
    let
        ( m, _ ) =
            step (TM.ListLoaded 3 1 "" (Ok { version = 1, tags = [], nextCursor = Nothing })) openedWithRows
    in
    List.length (TM.rows m) == 2


caseStale409 : Bool
caseStale409 =
    let
        ( m, outs ) =
            step (TM.ApplyCompleted 7 1 draft preview (Err (Http.BadStatus 409))) applying
    in
    outs == [] && TM.needsPreview m && not (TM.canConfirm m) && TM.currentPreview m == Nothing && TM.sourceTag m == "old"


caseStale409Recovery : Bool
caseStale409Recovery =
    let
        stale =
            applying |> step (TM.ApplyCompleted 7 1 draft preview (Err (Http.BadStatus 409))) |> Tuple.first

        previewing =
            stale |> step TM.PreviewRequested |> Tuple.first

        -- previewRequestId was 1, next is 2
        ( m, _ ) =
            step (TM.PreviewCompleted 7 4 draft (Ok { preview | version = 4 })) previewing
    in
    TM.canConfirm m


caseDoubleSubmit : Bool
caseDoubleSubmit =
    let
        ( m, outs ) =
            step TM.ApplyRequested applying
    in
    outs == [] && m == applying && TM.writePending m


caseMalformedReceiptUncertain : Bool
caseMalformedReceiptUncertain =
    let
        bad =
            { receipt | affectedPostCount = 4 }

        ( m, outs ) =
            step (TM.ApplyCompleted 7 1 draft preview (Ok bad)) applying

        ( blocked, _ ) =
            step TM.ApplyRequested m
    in
    outs == [ TM.ResultUncertain ] && TM.uncertain m && not (TM.canConfirm m) && TM.uncertain blocked && not (TM.writePending blocked)


caseTimeoutUncertain : Bool
caseTimeoutUncertain =
    let
        ( m, outs ) =
            step (TM.ApplyCompleted 7 1 draft preview (Err Http.Timeout)) applying
    in
    outs == [ TM.ResultUncertain ] && TM.uncertain m && not (TM.canConfirm m)


caseFreshPreviewRecovery : Bool
caseFreshPreviewRecovery =
    let
        uncertainModel =
            applying |> step (TM.ApplyCompleted 7 1 draft preview (Err Http.NetworkError)) |> Tuple.first

        previewing =
            uncertainModel |> step TM.PreviewRequested |> Tuple.first

        ( m, _ ) =
            step (TM.PreviewCompleted 7 4 draft (Ok { preview | version = 9 })) previewing
    in
    not (TM.uncertain m) && TM.canConfirm m


caseVisibilityDriftReceipt : Bool
caseVisibilityDriftReceipt =
    let
        drifted =
            { receipt | visiblePostCount = 0 }

        ( m, outs ) =
            step (TM.ApplyCompleted 7 1 draft preview (Ok drifted)) applying
    in
    outs == [ TM.Applied drifted ] && not (TM.uncertain m)


caseExactReceiptRejectedOnVersion : Bool
caseExactReceiptRejectedOnVersion =
    let
        ( m, outs ) =
            step (TM.ApplyCompleted 7 1 draft preview (Ok { receipt | version = 5 })) applying
    in
    outs == [ TM.ResultUncertain ] && TM.uncertain m


caseChangedFalseRejected : Bool
caseChangedFalseRejected =
    let
        ( m, outs ) =
            step (TM.ApplyCompleted 7 1 draft preview (Ok { receipt | changed = False })) applying
    in
    outs == [ TM.ResultUncertain ] && TM.uncertain m


caseLockClears : Bool
caseLockClears =
    let
        cleared =
            TM.locked openedWithRows

        -- also draft/preview clear
        cleared2 =
            TM.locked ready
    in
    TM.rows cleared == [] && not (TM.isOpen cleared) && TM.currentPreview cleared2 == Nothing && TM.sourceTag cleared2 == ""


caseCursorDedup : Bool
caseCursorDedup =
    let
        more =
            openedWithRows |> step TM.LoadMoreRequested |> Tuple.first

        -- Overlapping rows are suspect now: the page is not appended.
        ( m, _ ) =
            step (TM.ListLoaded 7 2 "" (Ok { version = 3, tags = [ { tag = "old", postCount = 9, visiblePostCount = 9 }, { tag = "third", postCount = 1, visiblePostCount = 0 } ], nextCursor = Nothing })) more
    in
    List.map .tag (TM.rows m) == [ "old", "other" ] && TM.nextCursor m == Nothing && not (TM.listLoading m)


caseRepeatedCursor : Bool
caseRepeatedCursor =
    let
        more =
            openedWithRows |> step TM.LoadMoreRequested |> Tuple.first

        ( m, _ ) =
            step (TM.ListLoaded 7 2 "" (Ok { version = 3, tags = [ { tag = "third", postCount = 1, visiblePostCount = 0 } ], nextCursor = Just "c1" })) more
    in
    TM.nextCursor m == Nothing && List.map .tag (TM.rows m) == [ "old", "other" ]


caseApply401 : Bool
caseApply401 =
    let
        ( _, outs ) =
            step (TM.ApplyCompleted 7 1 draft preview (Err (Http.BadStatus 401))) applying
    in
    outs == [ TM.AccessRevoked ]


caseList401 : Bool
caseList401 =
    let
        ( _, outs ) =
            step (TM.ListLoaded 7 1 "" (Err (Http.BadStatus 401))) (TM.init |> step TM.OpenRequested |> Tuple.first)
    in
    outs == [ TM.AccessRevoked ]


caseDecoderRejectsEmptyId : Bool
caseDecoderRejectsEmptyId =
    Decode.decodeString Domain.receiptDecoder """{"id":"","version":4,"mode":"rename","source_tag":"old","target_tag":"new","affected_post_count":5,"visible_post_count":2,"changed":true}""" |> Result.toMaybe |> (==) Nothing


caseDecoderRejectsMissingVersion : Bool
caseDecoderRejectsMissingVersion =
    Decode.decodeString Domain.previewDecoder """{"mode":"rename","source_tag":"old","target_tag":"new","affected_post_count":5,"visible_post_count":2,"target_post_count":0}""" |> Result.toMaybe |> (==) Nothing


caseDecoderRejectsNullVersion : Bool
caseDecoderRejectsNullVersion =
    Decode.decodeString Domain.listPageDecoder """{"version":null,"tags":[],"next_cursor":null}""" |> Result.toMaybe |> (==) Nothing


caseDecoderRejectsUnsafeVersion : Bool
caseDecoderRejectsUnsafeVersion =
    Decode.decodeString Domain.listPageDecoder """{"version":9007199254740993,"tags":[],"next_cursor":null}""" |> Result.toMaybe |> (==) Nothing


caseDecoderRejectsNegativeCount : Bool
caseDecoderRejectsNegativeCount =
    Decode.decodeString Domain.listPageDecoder """{"version":1,"tags":[{"tag":"a","post_count":-1,"visible_post_count":0}],"next_cursor":null}""" |> Result.toMaybe |> (==) Nothing


caseDecoderAcceptsList : Bool
caseDecoderAcceptsList =
    Decode.decodeString Domain.listPageDecoder """{"version":2,"tags":[{"tag":"a","post_count":3,"visible_post_count":1}],"next_cursor":"c"}"""
        == Ok { version = 2, tags = [ { tag = "a", postCount = 3, visiblePostCount = 1 } ], nextCursor = Just "c" }


caseChangedVersionAppendRejected : Bool
caseChangedVersionAppendRejected =
    let
        more =
            openedWithRows |> step TM.LoadMoreRequested |> Tuple.first

        ( m, _ ) =
            step (TM.ListLoaded 7 2 "" (Ok { version = 4, tags = [ { tag = "third", postCount = 1, visiblePostCount = 0 } ], nextCursor = Nothing })) more
    in
    List.map .tag (TM.rows m) == [ "old", "other" ] && TM.nextCursor m == Nothing


caseValidAppend : Bool
caseValidAppend =
    let
        more =
            openedWithRows |> step TM.LoadMoreRequested |> Tuple.first

        ( m, _ ) =
            step (TM.ListLoaded 7 2 "" (Ok { version = 3, tags = [ { tag = "third", postCount = 1, visiblePostCount = 0 } ], nextCursor = Nothing })) more
    in
    List.map .tag (TM.rows m) == [ "old", "other", "third" ] && TM.status m == Nothing


caseInternalDuplicatesFirst : Bool
caseInternalDuplicatesFirst =
    let
        opened =
            TM.init |> step TM.OpenRequested |> Tuple.first

        ( m, _ ) =
            step (TM.ListLoaded 7 1 "" (Ok { version = 3, tags = [ { tag = "a", postCount = 1, visiblePostCount = 1 }, { tag = "a", postCount = 2, visiblePostCount = 1 } ], nextCursor = Nothing })) opened
    in
    TM.rows m == [] && TM.nextCursor m == Nothing


caseInternalDuplicatesAppend : Bool
caseInternalDuplicatesAppend =
    let
        more =
            openedWithRows |> step TM.LoadMoreRequested |> Tuple.first

        ( m, _ ) =
            step (TM.ListLoaded 7 2 "" (Ok { version = 3, tags = [ { tag = "x", postCount = 1, visiblePostCount = 1 }, { tag = "x", postCount = 1, visiblePostCount = 0 } ], nextCursor = Nothing })) more
    in
    List.map .tag (TM.rows m) == [ "old", "other" ] && TM.nextCursor m == Nothing


caseOversizedPageRejected : Bool
caseOversizedPageRejected =
    let
        opened =
            TM.init |> step TM.OpenRequested |> Tuple.first

        bigPage =
            { version = 3, tags = List.map (\n -> { tag = "t" ++ String.fromInt n, postCount = 1, visiblePostCount = 0 }) (List.range 1 61), nextCursor = Nothing }

        ( m, _ ) =
            step (TM.ListLoaded 7 1 "" (Ok bigPage)) opened
    in
    TM.rows m == [] && TM.nextCursor m == Nothing


caseEmptyNonTerminalFirst : Bool
caseEmptyNonTerminalFirst =
    let
        opened =
            TM.init |> step TM.OpenRequested |> Tuple.first

        ( m, _ ) =
            step (TM.ListLoaded 7 1 "" (Ok { version = 3, tags = [], nextCursor = Just "c1" })) opened
    in
    TM.rows m == [] && TM.nextCursor m == Nothing && TM.status m /= Nothing


caseEmptyNonTerminalAppend : Bool
caseEmptyNonTerminalAppend =
    let
        more =
            openedWithRows |> step TM.LoadMoreRequested |> Tuple.first

        ( m, _ ) =
            step (TM.ListLoaded 7 2 "" (Ok { version = 3, tags = [], nextCursor = Just "c2" })) more
    in
    List.map .tag (TM.rows m) == [ "old", "other" ] && TM.nextCursor m == Nothing


caseFirstPageNewVersionAfterPrefix : Bool
caseFirstPageNewVersionAfterPrefix =
    let
        filtered =
            openedWithRows |> step (TM.PrefixChanged "o") |> Tuple.first

        ( m, _ ) =
            step (TM.ListLoaded 7 2 "o" (Ok { version = 99, tags = [ { tag = "oak", postCount = 1, visiblePostCount = 1 } ], nextCursor = Nothing })) filtered
    in
    List.map .tag (TM.rows m) == [ "oak" ]


caseSuccessfulApplyReloads : Bool
caseSuccessfulApplyReloads =
    let
        appliedModel =
            openedWithRows
                |> step (TM.SourceChosen "old") |> Tuple.first
                |> step (TM.TargetChanged "new") |> Tuple.first
                |> step TM.PreviewRequested |> Tuple.first
                |> step (TM.PreviewCompleted 7 4 draft (Ok preview)) |> Tuple.first
                |> step TM.ApplyRequested |> Tuple.first

        ( afterApply, outs ) =
            step (TM.ApplyCompleted 7 1 draft preview (Ok receipt)) appliedModel

        -- The reload is the next list request; it must not append stale rows.
        ( reloaded, _ ) =
            step (TM.ListLoaded 7 2 "" (Ok { version = 4, tags = [ { tag = "new", postCount = 5, visiblePostCount = 2 } ], nextCursor = Nothing })) afterApply
    in
    outs
        == [ TM.Applied receipt ]
        && TM.rows afterApply
        == []
        && TM.nextCursor afterApply
        == Nothing
        && TM.listLoading afterApply
        && TM.status afterApply
        == Just "Renamed old to new."
        && List.map .tag (TM.rows reloaded)
        == [ "new" ]
        && TM.status reloaded
        == Just "Renamed old to new."


caseOpenResetsPreview : Bool
caseOpenResetsPreview =
    let
        ( m, _ ) =
            step TM.OpenRequested ready
    in
    TM.currentPreview m == Nothing && not (TM.canConfirm m) && TM.rows m == []


caseCloseReopenLateListIgnored : Bool
caseCloseReopenLateListIgnored =
    let
        reopened =
            openedWithRows
                |> step TM.CloseRequested |> Tuple.first
                |> step TM.OpenRequested |> Tuple.first

        ( m, outs ) =
            step (TM.ListLoaded 7 1 "" (Ok { version = 3, tags = [ { tag = "stale", postCount = 1, visiblePostCount = 1 } ], nextCursor = Nothing })) reopened
    in
    outs == [] && TM.rows m == [] && m == reopened


caseCloseReopenLatePreviewIgnored : Bool
caseCloseReopenLatePreviewIgnored =
    let
        reopened =
            ready
                |> step TM.CloseRequested |> Tuple.first
                |> step TM.OpenRequested |> Tuple.first

        ( m, outs ) =
            step (TM.PreviewCompleted 7 3 draft (Ok preview)) reopened
    in
    outs == [] && TM.currentPreview m == Nothing && m == reopened


caseCloseReopenLateReceiptIgnored : Bool
caseCloseReopenLateReceiptIgnored =
    let
        reopened =
            ready
                |> step TM.CloseRequested |> Tuple.first
                |> step TM.OpenRequested |> Tuple.first

        ( m, outs ) =
            step (TM.ApplyCompleted 7 1 draft preview (Ok receipt)) reopened
    in
    outs == [] && not (TM.uncertain m) && m == reopened


caseBadBodyUncertain : Bool
caseBadBodyUncertain =
    let
        ( m, outs ) =
            step (TM.ApplyCompleted 7 1 draft preview (Err (Http.BadBody "bad json"))) applying
    in
    outs == [ TM.ResultUncertain ] && TM.uncertain m


caseSafeCountAcceptsBeyond32Bit : Bool
caseSafeCountAcceptsBeyond32Bit =
    Decode.decodeString Domain.safeCountDecoder "2147483648" == Ok 2147483648


caseSafeCountRejectsUnsafe : Bool
caseSafeCountRejectsUnsafe =
    Decode.decodeString Domain.safeCountDecoder "9007199254740992" |> Result.toMaybe |> (==) Nothing


caseSafeCountRejectsFraction : Bool
caseSafeCountRejectsFraction =
    Decode.decodeString Domain.safeCountDecoder "1.5" |> Result.toMaybe |> (==) Nothing


caseSafeCountRejectsNull : Bool
caseSafeCountRejectsNull =
    Decode.decodeString Domain.safeCountDecoder "null" |> Result.toMaybe |> (==) Nothing


caseValidateTagEmoji : Bool
caseValidateTagEmoji =
    (Domain.validateTag (String.repeat 128 "🎉") |> Result.toMaybe |> (/=) Nothing)
        && (Domain.validateTag (String.repeat 129 "🎉") == Err "Tags are at most 128 characters.")


previewJson : String -> String -> String -> String
previewJson mode affected visible =
    "{\"version\":3,\"mode\":\"" ++ mode ++ "\",\"source_tag\":\"old\",\"target_tag\":\"new\",\"affected_post_count\":" ++ affected ++ ",\"visible_post_count\":" ++ visible ++ ",\"target_post_count\":"


casePreviewDecoderBounds : Bool
casePreviewDecoderBounds =
    List.all
        (\json -> (Decode.decodeString Domain.previewDecoder json |> Result.toMaybe) == Nothing)
        [ previewJson "rename" "0" "0" ++ "0}"
        , previewJson "rename" "1001" "0" ++ "0}"
        , previewJson "rename" "5" "6" ++ "0}"
        , previewJson "rename" "5" "2" ++ "1}"
        , previewJson "merge" "5" "2" ++ "0}"
        , "{\"version\":3,\"mode\":\"rename\",\"source_tag\":\"old\",\"target_tag\":\"old\",\"affected_post_count\":5,\"visible_post_count\":2,\"target_post_count\":0}"
        ]
        && (Decode.decodeString Domain.previewDecoder (previewJson "merge" "5" "2" ++ "3}") |> Result.toMaybe |> (/=) Nothing)


receiptJson : String -> String -> String -> String -> String
receiptJson id affected visible changed =
    "{\"id\":\"" ++ id ++ "\",\"version\":4,\"mode\":\"rename\",\"source_tag\":\"old\",\"target_tag\":\"new\",\"affected_post_count\":" ++ affected ++ ",\"visible_post_count\":" ++ visible ++ ",\"changed\":" ++ changed ++ "}"


caseReceiptDecoderRules : Bool
caseReceiptDecoderRules =
    List.all
        (\json -> (Decode.decodeString Domain.receiptDecoder json |> Result.toMaybe) == Nothing)
        [ receiptJson "op-1" "5" "2" "true"
        , receiptJson "01" "5" "2" "true"
        , receiptJson "0" "5" "2" "true"
        , receiptJson "1" "0" "0" "true"
        , receiptJson "1" "1001" "0" "true"
        , receiptJson "1" "5" "6" "true"
        , receiptJson "1" "5" "2" "false"
        ]
        && (Decode.decodeString Domain.receiptDecoder (receiptJson "1" "5" "2" "true") == Ok receipt)


caseReceiptMatchesOverflow : Bool
caseReceiptMatchesOverflow =
    not (Domain.receiptMatchesPreview { preview | version = 9007199254740991 } { receipt | version = 9007199254740992 })
        && not (Domain.receiptMatchesPreview preview { receipt | visiblePostCount = 6 })
        && not (Domain.receiptMatchesPreview preview { receipt | id = "op-1" })
        && Domain.receiptMatchesPreview preview receipt


caseTagRowDecoderRules : Bool
caseTagRowDecoderRules =
    List.all
        (\json -> (Decode.decodeString Domain.listPageDecoder json |> Result.toMaybe) == Nothing)
        [ "{\"version\":1,\"tags\":[{\"tag\":\"a\",\"post_count\":0,\"visible_post_count\":0}],\"next_cursor\":null}"
        , "{\"version\":1,\"tags\":[{\"tag\":\"a\",\"post_count\":2,\"visible_post_count\":3}],\"next_cursor\":null}"
        ]


caseCloseDuringWriteBlocked : Bool
caseCloseDuringWriteBlocked =
    let
        opened =
            { applying | open = True }

        ( m, _ ) =
            step TM.CloseRequested opened
    in
    m == opened


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "tagmanage: ordinary tag changes invalidate preview", ready |> step TM.TagsChanged |> Tuple.first |> TM.canConfirm |> not )
    , ( "tagmanage: ordinary tag changes refresh the open list", openedWithRows |> step TM.TagsChanged |> Tuple.first |> TM.rows |> List.isEmpty )
    , ( "tagmanage: editing target invalidates preview", caseEditingInvalidates )
    , ( "tagmanage: editing source invalidates preview", caseEditingSourceInvalidates )
    , ( "tagmanage: mode change invalidates preview", caseModeInvalidates )
    , ( "tagmanage: prefix change invalidates preview", casePrefixInvalidates )
    , ( "tagmanage: late preview ignored", caseLatePreviewIgnored )
    , ( "tagmanage: late list ignored", caseLateListIgnored )
    , ( "tagmanage: late receipt access generation ignored", caseLateReceiptGenerationIgnored )
    , ( "tagmanage: list access generation ignored", caseAccessGenerationListIgnored )
    , ( "tagmanage: stale 409 demands new preview", caseStale409 )
    , ( "tagmanage: stale 409 fresh preview recovers", caseStale409Recovery )
    , ( "tagmanage: double submit blocked", caseDoubleSubmit )
    , ( "tagmanage: malformed receipt locks uncertain", caseMalformedReceiptUncertain )
    , ( "tagmanage: timeout locks uncertain", caseTimeoutUncertain )
    , ( "tagmanage: fresh preview recovers uncertain", caseFreshPreviewRecovery )
    , ( "tagmanage: receipt allows visibility drift", caseVisibilityDriftReceipt )
    , ( "tagmanage: receipt version must be preview+1", caseExactReceiptRejectedOnVersion )
    , ( "tagmanage: changed false receipt rejected", caseChangedFalseRejected )
    , ( "tagmanage: lock clears private list and draft", caseLockClears )
    , ( "tagmanage: overlapping page rejected not deduped", caseCursorDedup )
    , ( "tagmanage: repeated cursor stopped and not appended", caseRepeatedCursor )
    , ( "tagmanage: changed version append rejected", caseChangedVersionAppendRejected )
    , ( "tagmanage: valid append still works", caseValidAppend )
    , ( "tagmanage: internal duplicates first page rejected", caseInternalDuplicatesFirst )
    , ( "tagmanage: internal duplicates append rejected", caseInternalDuplicatesAppend )
    , ( "tagmanage: oversized page rejected", caseOversizedPageRejected )
    , ( "tagmanage: empty nonterminal first page rejected", caseEmptyNonTerminalFirst )
    , ( "tagmanage: empty nonterminal append rejected", caseEmptyNonTerminalAppend )
    , ( "tagmanage: first page new version after prefix accepted", caseFirstPageNewVersionAfterPrefix )
    , ( "tagmanage: successful apply clears list and reloads preserving notice", caseSuccessfulApplyReloads )
    , ( "tagmanage: open resets preview", caseOpenResetsPreview )
    , ( "tagmanage: close reopen late list ignored", caseCloseReopenLateListIgnored )
    , ( "tagmanage: close reopen late preview ignored", caseCloseReopenLatePreviewIgnored )
    , ( "tagmanage: close reopen late receipt ignored", caseCloseReopenLateReceiptIgnored )
    , ( "tagmanage: malformed apply body emits uncertain out", caseBadBodyUncertain )
    , ( "tagmanage: safe count accepts beyond 32 bit", caseSafeCountAcceptsBeyond32Bit )
    , ( "tagmanage: safe count rejects unsafe integer", caseSafeCountRejectsUnsafe )
    , ( "tagmanage: safe count rejects fraction", caseSafeCountRejectsFraction )
    , ( "tagmanage: safe count rejects null", caseSafeCountRejectsNull )
    , ( "tagmanage: emoji tag length counts code points", caseValidateTagEmoji )
    , ( "tagmanage: preview decoder enforces bounds", casePreviewDecoderBounds )
    , ( "tagmanage: receipt decoder enforces id and counts", caseReceiptDecoderRules )
    , ( "tagmanage: receipt match guards overflow and drift", caseReceiptMatchesOverflow )
    , ( "tagmanage: tag row decoder enforces counts", caseTagRowDecoderRules )
    , ( "tagmanage: apply 401 revokes", caseApply401 )
    , ( "tagmanage: list 401 revokes", caseList401 )
    , ( "tagmanage: decoder rejects empty id", caseDecoderRejectsEmptyId )
    , ( "tagmanage: decoder rejects missing version", caseDecoderRejectsMissingVersion )
    , ( "tagmanage: decoder rejects null version", caseDecoderRejectsNullVersion )
    , ( "tagmanage: decoder rejects unsafe version", caseDecoderRejectsUnsafeVersion )
    , ( "tagmanage: decoder rejects negative count", caseDecoderRejectsNegativeCount )
    , ( "tagmanage: decoder accepts list page", caseDecoderAcceptsList )
    , ( "tagmanage: close blocked while writing", caseCloseDuringWriteBlocked )
    ]
