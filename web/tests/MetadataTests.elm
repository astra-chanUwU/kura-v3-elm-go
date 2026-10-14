module MetadataTests exposing (allPassed, suite)

import Domain.Rating
import Api.Metadata as MetadataApi
import Domain.Post as Post
import Feature.Metadata as Metadata
import Http
import Json.Decode as Decode
import Json.Encode as Encode


detail : Int -> String -> String -> Post.PostDetail
detail version source artist =
    { id = "p1"
    , previewUrl = "/p"
    , originalUrl = "/o"
    , mediaType = "image/jpeg"
    , width = 1
    , height = 1
    , source = source
    , artist = artist
    , hash = "h"
    , fileSize = 1
    , createdAt = "2026-01-01"
    , tags = [ "kept" ]
    , tagVersion = 3
    , favorite = True
    , score = 5
    , reactionVersion = 2
    , history = []
    , reactionHistory = []
    , derivativeJobId = Nothing
    , derivativeStatus = Nothing
    , metadataVersion = version
    , metadataHistory = []
    , rating = Domain.Rating.Unrated
    , ratingVersion = Just 0
    , ratingHistory = []
    }


ctx : Maybe Post.PostDetail -> Metadata.Context
ctx activeDetail =
    Metadata.context "" (Just "tok") True 7 (Just "p1") activeDetail


detailFor : String -> Int -> String -> String -> Post.PostDetail
detailFor postId version source artist =
    let
        base =
            detail version source artist
    in
    { base | id = postId }


ctxFor : String -> Maybe Post.PostDetail -> Metadata.Context
ctxFor postId activeDetail =
    Metadata.context "" (Just "tok") True 7 (Just postId) activeDetail


lockedCtx : Metadata.Context
lockedCtx =
    Metadata.context "" Nothing False 7 (Just "p1") (Just (detail 0 "old" "artist"))


step : Metadata.Context -> Metadata.Msg -> Metadata.Model -> ( Metadata.Model, List Metadata.OutMsg )
step context msg model =
    let
        ( next, _, outs ) =
            Metadata.update context msg model
    in
    ( next, outs )


synced : Metadata.Model
synced =
    Metadata.syncDetail (detail 0 "old" "artist") Metadata.init


savingModel : Metadata.Model
savingModel =
    let
        drafted =
            step (ctx (Just (detail 0 "old" "artist"))) (Metadata.SourceChanged "new") synced |> Tuple.first
    in
    step (ctx (Just (detail 0 "old" "artist"))) Metadata.SaveRequested drafted |> Tuple.first


savedResponse : Post.MetadataResponse
savedResponse =
    { posts = [ { id = "p1", version = 1, source = "new", artist = "artist", changed = True } ] }


caseLegacyDefaults : Bool
caseLegacyDefaults =
    case Decode.decodeString Post.detailDecoder """{"id":"1","preview_url":"/p","original_url":"/o","media_type":"image/jpeg","width":1,"height":1,"source":"s","artist":"a","hash":"h","file_size":1,"created_at":"2026-01-01","tags":[]}""" of
        Ok decoded ->
            decoded.metadataVersion == 0 && decoded.metadataHistory == []

        Err _ ->
            False


caseMetadataFieldsDecode : Bool
caseMetadataFieldsDecode =
    case Decode.decodeString Post.detailDecoder """{"id":"1","preview_url":"/p","original_url":"/o","media_type":"image/jpeg","width":1,"height":1,"source":"s","artist":"a","hash":"h","file_size":1,"created_at":"2026-01-01","tags":[],"metadata_version":4,"metadata_history":[{"version":4,"previous_source":"x","previous_artist":"y","source":"s","artist":"a","created_at":"2026-02-02"}]}""" of
        Ok decoded ->
            decoded.metadataVersion == 4
                && (case decoded.metadataHistory of
                        [ revision ] ->
                            revision.previousSource == "x" && revision.source == "s" && revision.version == 4

                        _ ->
                            False
                   )

        Err _ ->
            False


caseEncodeOmittedVsEmpty : Bool
caseEncodeOmittedVsEmpty =
    let
        omitted =
            Encode.encode 0 (MetadataApi.encodeRequest [ { id = "p1", version = 0 } ] Nothing Nothing)

        clearing =
            Encode.encode 0 (MetadataApi.encodeRequest [ { id = "p1", version = 0 } ] (Just "") Nothing)
    in
    not (String.contains "\"source\"" omitted)
        && String.contains "\"source\":\"\"" clearing
        && String.contains "\"version\":0" omitted
        && MetadataApi.endpoint "" == "/api/posts/metadata"


caseSyncAndSaveVersionZero : Bool
caseSyncAndSaveVersionZero =
    Metadata.sourceDraft synced == "old"
        && Metadata.saving savingModel
        && Metadata.requestId savingModel == 1


caseNoopUnchanged : Bool
caseNoopUnchanged =
    let
        ( next, outs ) =
            step (ctx (Just (detail 0 "old" "artist"))) Metadata.SaveRequested synced
    in
    outs == [] && not (Metadata.saving next) && Metadata.status next == Just "Source and artist are unchanged."


caseEmptyClearsStartsSave : Bool
caseEmptyClearsStartsSave =
    let
        drafted =
            step (ctx (Just (detail 2 "old" "artist"))) (Metadata.SourceChanged "") (Metadata.syncDetail (detail 2 "old" "artist") Metadata.init) |> Tuple.first

        ( next, _ ) =
            step (ctx (Just (detail 2 "old" "artist"))) Metadata.SaveRequested drafted
    in
    Metadata.saving next


caseActiveChangeResets : Bool
caseActiveChangeResets =
    let
        drafted =
            step (ctx (Just (detail 0 "old" "artist"))) (Metadata.SourceChanged "draft") synced |> Tuple.first

        reset =
            Metadata.activeChanged drafted
    in
    Metadata.sourceDraft reset == "" && Metadata.artistDraft reset == "" && Metadata.status reset == Nothing && not (Metadata.conflict reset)


caseSaveSuccess : Bool
caseSaveSuccess =
    let
        ( next, outs ) =
            step (ctx (Just (detail 0 "old" "artist"))) (Metadata.Completed 7 (Metadata.requestId savingModel) "p1" (Ok savedResponse)) savingModel
    in
    outs == [ Metadata.Saved { id = "p1", version = 1, source = "new", artist = "artist", changed = True } ]
        && Metadata.sourceDraft next == "new"
        && not (Metadata.saving next)
        && Metadata.status next == Just "Source and artist saved."


caseNoopServerResult : Bool
caseNoopServerResult =
    let
        ( next, _ ) =
            step (ctx (Just (detail 0 "old" "artist")))
                (Metadata.Completed 7 (Metadata.requestId savingModel) "p1" (Ok { posts = [ { id = "p1", version = 0, source = "old", artist = "artist", changed = False } ] }))
                savingModel
    in
    Metadata.status next == Just "No changes were needed." && not (Metadata.saving next)


caseConflictKeepsDraft : Bool
caseConflictKeepsDraft =
    let
        ( next, outs ) =
            step (ctx (Just (detail 0 "old" "artist"))) (Metadata.Completed 7 (Metadata.requestId savingModel) "p1" (Err (Http.BadStatus 409))) savingModel
    in
    outs == []
        && Metadata.conflict next
        && Metadata.sourceDraft next == "new"
        && not (Metadata.saving next)
        && String.contains "reload" (Metadata.status next |> Maybe.withDefault "")


caseWrongPostRejected : Bool
caseWrongPostRejected =
    let
        ( next, outs ) =
            step (ctx (Just (detail 0 "old" "artist")))
                (Metadata.Completed 7 (Metadata.requestId savingModel) "p1" (Ok { posts = [ { id = "other", version = 1, source = "x", artist = "y", changed = True } ] }))
                savingModel
    in
    outs == [] && not (Metadata.saving next) && Metadata.sourceDraft next == "new"


caseMissingResultRejected : Bool
caseMissingResultRejected =
    let
        ( next, outs ) =
            step (ctx (Just (detail 0 "old" "artist"))) (Metadata.Completed 7 (Metadata.requestId savingModel) "p1" (Ok { posts = [] })) savingModel
    in
    outs == [] && String.contains "did not include" (Metadata.status next |> Maybe.withDefault "")


caseLateEpochStale : Bool
caseLateEpochStale =
    let
        ( next, outs ) =
            step (ctx (Just (detail 0 "old" "artist"))) (Metadata.Completed 3 (Metadata.requestId savingModel) "p1" (Ok savedResponse)) savingModel
    in
    outs == [ Metadata.StaleResponse "p1" ] && next == savingModel


caseUnauthorizedCurrent : Bool
caseUnauthorizedCurrent =
    let
        ( _, outs ) =
            step (ctx (Just (detail 0 "old" "artist"))) (Metadata.Completed 7 (Metadata.requestId savingModel) "p1" (Err (Http.BadStatus 401))) savingModel
    in
    outs == [ Metadata.AccessRevoked ]


caseStaleRequestIgnored : Bool
caseStaleRequestIgnored =
    let
        ( next, outs ) =
            step (ctx (Just (detail 0 "old" "artist"))) (Metadata.Completed 7 999 "p1" (Ok savedResponse)) savingModel
    in
    outs == [] && next == savingModel


caseLockedSave : Bool
caseLockedSave =
    let
        ( next, outs ) =
            step lockedCtx Metadata.SaveRequested synced
    in
    outs == [] && not (Metadata.saving next) && Metadata.status next == Just "Unlock to edit source and artist."


caseTooLong : Bool
caseTooLong =
    let
        drafted =
            step (ctx (Just (detail 0 "old" "artist"))) (Metadata.SourceChanged (String.repeat 513 "a")) synced |> Tuple.first

        ( next, _ ) =
            step (ctx (Just (detail 0 "old" "artist"))) Metadata.SaveRequested drafted
    in
    not (Metadata.saving next) && String.contains "512" (Metadata.status next |> Maybe.withDefault "")


caseMissingPostMessage : Bool
caseMissingPostMessage =
    Metadata.errorToString (Http.BadStatus 404) == "This post no longer exists or is not visible."
        && (let
                ( next, _ ) =
                    step (ctx (Just (detail 0 "old" "artist"))) (Metadata.Completed 7 (Metadata.requestId savingModel) "p1" (Err (Http.BadStatus 404))) savingModel
            in
            Metadata.status next == Just "This post no longer exists or is not visible."
           )


caseLockedClearsSaving : Bool
caseLockedClearsSaving =
    not (Metadata.saving (Metadata.locked savingModel))


caseSyncWhileSavingKeepsDraft : Bool
caseSyncWhileSavingKeepsDraft =
    Metadata.sourceDraft (Metadata.syncDetail (detail 5 "server" "artist") savingModel) == "new"


caseNavigateDuringSaveLoadsAndSavesB : Bool
caseNavigateDuringSaveLoadsAndSavesB =
    let
        oldId =
            Metadata.requestId savingModel

        navigated =
            Metadata.activeChanged savingModel

        loadedB =
            Metadata.syncDetail (detailFor "p2" 0 "b-source" "b-artist") navigated

        ( afterOldAck, oldOuts ) =
            step (ctxFor "p2" (Just (detailFor "p2" 0 "b-source" "b-artist")))
                (Metadata.Completed 7 oldId "p1" (Ok savedResponse))
                loadedB

        draftedB =
            step (ctxFor "p2" (Just (detailFor "p2" 0 "b-source" "b-artist"))) (Metadata.SourceChanged "b-new") afterOldAck |> Tuple.first

        ( savingB, _ ) =
            step (ctxFor "p2" (Just (detailFor "p2" 0 "b-source" "b-artist"))) Metadata.SaveRequested draftedB

        ( afterOldAckDuringB, oldOutsDuringB ) =
            step (ctxFor "p2" (Just (detailFor "p2" 0 "b-source" "b-artist")))
                (Metadata.Completed 7 oldId "p1" (Ok savedResponse))
                savingB
    in
    not (Metadata.saving navigated)
        && Metadata.requestId navigated == oldId + 1
        && Metadata.sourceDraft loadedB == "b-source"
        && Metadata.artistDraft loadedB == "b-artist"
        && oldOuts == [ Metadata.StaleResponse "p1" ]
        && not (Metadata.saving afterOldAck)
        && Metadata.sourceDraft afterOldAck == "b-source"
        && Metadata.saving savingB
        && Metadata.sourceDraft savingB == "b-new"
        && Metadata.requestId savingB == oldId + 2
        && oldOutsDuringB == [ Metadata.StaleResponse "p1" ]
        && Metadata.saving afterOldAckDuringB
        && Metadata.sourceDraft afterOldAckDuringB == "b-new"


caseSyncPreservesUnsavedDraft : Bool
caseSyncPreservesUnsavedDraft =
    let
        drafted =
            step (ctx (Just (detail 0 "old" "artist"))) (Metadata.SourceChanged "draft") synced |> Tuple.first

        refreshed =
            Metadata.syncDetail (detail 1 "server" "other") drafted
    in
    Metadata.sourceDraft refreshed == "draft" && Metadata.artistDraft refreshed == "artist"


caseSyncAfterSavePreservesNextDraft : Bool
caseSyncAfterSavePreservesNextDraft =
    let
        ( saved, _ ) =
            step (ctx (Just (detail 0 "old" "artist"))) (Metadata.Completed 7 (Metadata.requestId savingModel) "p1" (Ok savedResponse)) savingModel

        drafted =
            step (ctx (Just (detail 1 "new" "artist"))) (Metadata.SourceChanged "another") saved |> Tuple.first

        refreshed =
            Metadata.syncDetail (detail 1 "new" "artist") drafted
    in
    Metadata.sourceDraft refreshed == "another"


caseExplicitReloadResetsDraft : Bool
caseExplicitReloadResetsDraft =
    let
        ( conflicted, _ ) =
            step (ctx (Just (detail 0 "old" "artist"))) (Metadata.Completed 7 (Metadata.requestId savingModel) "p1" (Err (Http.BadStatus 409))) savingModel

        ( reloading, outs ) =
            step (ctx (Just (detail 0 "old" "artist"))) Metadata.ReloadRequested conflicted

        refreshed =
            Metadata.syncDetail (detail 9 "reloaded" "artist") reloading
    in
    outs == [ Metadata.ReloadPost "p1" ]
        && Metadata.sourceDraft refreshed == "reloaded"
        && not (Metadata.conflict refreshed)


caseCompletedAfterActiveChangeStale : Bool
caseCompletedAfterActiveChangeStale =
    let
        otherCtx =
            Metadata.context "" (Just "tok") True 7 (Just "p2") (Just (detail 0 "other" "artist"))

        ( next, outs ) =
            step otherCtx (Metadata.Completed 7 (Metadata.requestId savingModel) "p1" (Ok savedResponse)) savingModel
    in
    outs == [ Metadata.StaleResponse "p1" ] && not (Metadata.saving next) && Metadata.sourceDraft next == "new"


caseDirtyPayloadKeepsBaseVersion : Bool
caseDirtyPayloadKeepsBaseVersion =
    let
        drafted =
            step (ctx (Just (detail 0 "old" "artist"))) (Metadata.SourceChanged "edited source") synced |> Tuple.first

        refreshed =
            Metadata.syncDetail (detail 1 "old" "External artist") drafted
    in
    case Metadata.savePayload refreshed of
        Just payload ->
            payload.version == 0
                && payload.source == Just "edited source"
                && payload.artist == Nothing
                && (MetadataApi.encodeRequest [ { id = "p1", version = payload.version } ] payload.source payload.artist |> Encode.encode 0)
                == "{\"posts\":[{\"id\":\"p1\",\"version\":0}],\"source\":\"edited source\"}"

        Nothing ->
            False


caseReloadRebasesPayload : Bool
caseReloadRebasesPayload =
    let
        ( reloading, _ ) =
            step (ctx (Just (detail 1 "latest" "External artist"))) Metadata.ReloadRequested synced

        refreshed =
            Metadata.syncDetail (detail 1 "latest" "External artist") reloading

        ( drafted, _ ) =
            step (ctx (Just (detail 1 "latest" "External artist"))) (Metadata.ArtistChanged "") refreshed
    in
    Metadata.savePayload drafted == Just { version = 1, source = Nothing, artist = Just "" }


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "metadata legacy detail defaults version and history", caseLegacyDefaults )
    , ( "metadata detail fields and history decode", caseMetadataFieldsDecode )
    , ( "metadata encode omitted versus empty and explicit zero", caseEncodeOmittedVsEmpty )
    , ( "metadata syncs drafts and saves with version zero", caseSyncAndSaveVersionZero )
    , ( "metadata unchanged save is a local noop", caseNoopUnchanged )
    , ( "metadata empty draft clears and starts save", caseEmptyClearsStartsSave )
    , ( "metadata active change resets drafts", caseActiveChangeResets )
    , ( "metadata save success settles drafts", caseSaveSuccess )
    , ( "metadata server noop keeps version message", caseNoopServerResult )
    , ( "metadata conflict keeps draft and prompts reload", caseConflictKeepsDraft )
    , ( "metadata wrong post result rejected", caseWrongPostRejected )
    , ( "metadata missing result rejected", caseMissingResultRejected )
    , ( "metadata late epoch is stale not saved", caseLateEpochStale )
    , ( "metadata current 401 revokes", caseUnauthorizedCurrent )
    , ( "metadata stale request ignored", caseStaleRequestIgnored )
    , ( "metadata locked save asks to unlock", caseLockedSave )
    , ( "metadata values capped at 512 characters", caseTooLong )
    , ( "metadata missing post message", caseMissingPostMessage )
    , ( "metadata lock clears saving", caseLockedClearsSaving )
    , ( "metadata sync while saving keeps draft", caseSyncWhileSavingKeepsDraft )
    , ( "metadata completion for non-active post is stale", caseCompletedAfterActiveChangeStale )
    , ( "metadata navigate during save loads and saves next post independently", caseNavigateDuringSaveLoadsAndSavesB )
    , ( "metadata background refresh preserves unsaved draft", caseSyncPreservesUnsavedDraft )
    , ( "metadata refresh after save preserves next draft", caseSyncAfterSavePreservesNextDraft )
    , ( "metadata dirty save payload keeps base version and omits untouched artist", caseDirtyPayloadKeepsBaseVersion )
    , ( "metadata explicit reload rebases version and clears artist explicitly", caseReloadRebasesPayload )
    , ( "metadata explicit reload resets draft", caseExplicitReloadResetsDraft )
    ]
