module Feature.Similarity exposing
    ( Context
    , Model
    , Msg(..)
    , OutMsg(..)
    , anchor
    , anchorId
    , context
    , currentThreshold
    , indexCounts
    , indexingLibrary
    , init
    , isBusy
    , isOpen
    , locked
    , matches
    , members
    , polling
    , queueGeneration
    , requestId
    , resultUncertain
    , selectedIds
    , status
    , trashableIds
    , update
    , view
    )

{-| Bounded visual near-copy review around one explicit anchor post.

Matches page at 30 (server order: distance, then numeric id) under an
explicitly applied distance threshold (0..16). A candidate may be a
resized or recompressed copy; this is not semantic, crop, or flip
matching, and even distance 0 can be a false positive — byte identity
belongs to the exact-copies UI, so nothing here is labelled identical
and nothing is chosen or trashed automatically.

The selection is this module's own set, capped at 100 loaded ids.
Indexing is explicit derived-data work: opening never writes. Indexing
the anchor polls its job for at most 60s, then waits for an explicit
Reload. Indexing the library scans protected status pages (100 at a
time) and queues only missing, not-failed posts in sequential batches
of 100; cancelling stops further batches while already-queued jobs
remain. An uncertain POST result blocks re-queueing until the user
reviews it. "Index this post" is the explicit retry for a terminally
failed fingerprint job: the library build never requeues failed jobs,
and a failed poll is surfaced with its error so the user can fix the
cause and retry explicitly. Late callbacks are bound to the access
generation, a request id, the anchor, and the applied threshold.
-}

import Api.Job
import Api.Post
import Api.Similarity
import Domain.Post exposing (PostSummary)
import Domain.Similarity exposing (IndexResponse, SimilarMatch, SimilarPage, StatusPage)
import Html exposing (Html, button, div, input, label, li, p, span, text, ul)
import Html.Attributes as Attr
import Html.Attributes exposing (attribute, checked, class, disabled, title, type_, value)
import Html.Events exposing (onCheck, onClick, onInput)
import Http
import Process
import Set exposing (Set)
import Task
import Ui.Media


pageLimit : Int
pageLimit =
    30


maxSelected : Int
maxSelected =
    100


batchLimit : Int
batchLimit =
    100


maxPollAttempts : Int
maxPollAttempts =
    30


type alias Context =
    { apiBase : String
    , credential : Maybe String
    , writesEnabled : Bool
    , accessGeneration : Int
    }


context : String -> Maybe String -> Bool -> Int -> Context
context apiBase credential writesEnabled accessGeneration =
    { apiBase = apiBase, credential = credential, writesEnabled = writesEnabled, accessGeneration = accessGeneration }


type LibraryPhase
    = LibraryIdle
    | LibraryScanning
    | LibraryQueuing


type alias Model =
    { open : Bool
    , anchor : Maybe PostSummary
    , threshold : Int
    , thresholdInput : Int
    , matches : List SimilarMatch
    , nextCursor : Maybe String
    , loading : Bool
    , requestId : Int
    , indexed : Bool
    , selected : Set String
    , missing : Set String
    , status : Maybe String
    , uncertain : Bool
    , posting : Bool
    , pollJob : Maybe String
    , pollId : Int
    , pollAttempts : Int
    , libraryPhase : LibraryPhase
    , missingQueue : List String
    , queuedCount : Int
    , missingTotal : Int
    , indexedCount : Int
    , eligibleCount : Int
    , queueGeneration : Int
    }


init : Model
init =
    { open = False
    , anchor = Nothing
    , threshold = 8
    , thresholdInput = 8
    , matches = []
    , nextCursor = Nothing
    , loading = False
    , requestId = 0
    , indexed = False
    , selected = Set.empty
    , missing = Set.empty
    , status = Nothing
    , uncertain = False
    , posting = False
    , pollJob = Nothing
    , pollId = 0
    , pollAttempts = 0
    , libraryPhase = LibraryIdle
    , missingQueue = []
    , queuedCount = 0
    , missingTotal = 0
    , indexedCount = 0
    , eligibleCount = 0
    , queueGeneration = 0
    }


isOpen : Model -> Bool
isOpen model =
    model.open


anchor : Model -> Maybe PostSummary
anchor model =
    model.anchor


anchorId : Model -> Maybe String
anchorId model =
    Maybe.map .id model.anchor


matches : Model -> List SimilarMatch
matches model =
    model.matches


members : Model -> List PostSummary
members model =
    List.map .post model.matches


currentThreshold : Model -> Int
currentThreshold model =
    model.threshold


status : Model -> Maybe String
status model =
    model.status


requestId : Model -> Int
requestId model =
    model.requestId


queueGeneration : Model -> Int
queueGeneration model =
    model.queueGeneration


resultUncertain : Model -> Bool
resultUncertain model =
    model.uncertain


polling : Model -> Bool
polling model =
    model.pollJob /= Nothing


indexingLibrary : Model -> Bool
indexingLibrary model =
    model.libraryPhase /= LibraryIdle


indexCounts : Model -> ( Int, Int )
indexCounts model =
    ( model.indexedCount, model.eligibleCount )


isBusy : Model -> Bool
isBusy model =
    model.loading || model.posting || polling model || indexingLibrary model


selectedIds : Model -> List String
selectedIds model =
    model.matches |> List.map (.id << .post) |> List.filter (\id -> Set.member id model.selected)


trashableIds : Model -> List String
trashableIds model =
    selectedIds model |> List.take maxSelected


{-| Lock/revoke clears the pending write selection and stops all
indexing work and polling. Public read state stays readable; in-flight
responses go stale through bumped request/poll/queue generations.
-}
locked : Model -> Model
locked model =
    { model
        | selected = Set.empty
        , loading = False
        , posting = False
        , pollJob = Nothing
        , libraryPhase = LibraryIdle
        , missingQueue = []
        , requestId = model.requestId + 1
        , pollId = model.pollId + 1
        , queueGeneration = model.queueGeneration + 1
    }


type Msg
    = OpenRequested PostSummary
    | CloseRequested
    | ReloadRequested
    | LoadMoreRequested
    | ThresholdChanged Int
    | SimilarLoaded Int Int String Int Bool (Result Http.Error SimilarPage)
    | MatchToggled String Bool
    | SelectionCleared
    | CompareRequested String
    | TrashRequested
    | MediaFailed String
    | IndexAnchorRequested
    | IndexAnchorLoaded Int Int (Result Http.Error IndexResponse)
    | PollTick Int Int
    | JobLoaded Int Int (Result Http.Error Api.Job.Job)
    | IndexLibraryRequested
    | StatusScanLoaded Int (Result Http.Error StatusPage)
    | IndexBatchLoaded Int (List String) (Result Http.Error IndexResponse)
    | StatusCountsLoaded Int (Result Http.Error StatusPage)
    | CancelIndexLibraryRequested
    | UncertainReviewed
    | TrashApplied (List String)
    | RestoreApplied (List String)
    | VisibilityChanged


type OutMsg
    = AccessRevoked
    | ResultUncertain String
    | CompareCopies PostSummary PostSummary (List PostSummary)
    | TrashSelected (List String)


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        OpenRequested summary ->
            if not (Domain.Similarity.isCanonicalId summary.id) then
                ( { model | status = Just "That post cannot be reviewed here." }, Cmd.none, [] )

            else if model.open && anchorId model == Just summary.id then
                ( model, Cmd.none, [] )

            else
                let
                    id =
                        model.requestId + 1

                    fresh =
                        { init
                            | open = True
                            , anchor = Just summary
                            , threshold = model.threshold
                            , thresholdInput = model.threshold
                            , loading = True
                            , requestId = id
                            , queueGeneration = model.queueGeneration + 1
                            , status = Just "Loading similar posts…"
                        }
                in
                ( fresh
                , Api.Similarity.listSimilar ctx.apiBase summary.id model.threshold pageLimit Nothing
                    (SimilarLoaded ctx.accessGeneration id summary.id model.threshold False)
                , []
                )

        CloseRequested ->
            ( { init | threshold = model.threshold, thresholdInput = model.thresholdInput, requestId = model.requestId + 1, queueGeneration = model.queueGeneration + 1 }
            , Cmd.none
            , []
            )

        ReloadRequested ->
            case model.anchor of
                Just summary ->
                    if model.loading then
                        ( model, Cmd.none, [] )

                    else
                        let
                            applied =
                                clampThreshold model.thresholdInput

                            id =
                                model.requestId + 1
                        in
                        ( { model | open = True, threshold = applied, loading = True, requestId = id, pollJob = Nothing, pollId = model.pollId + 1, status = Just "Loading similar posts…" }
                        , Api.Similarity.listSimilar ctx.apiBase summary.id applied pageLimit Nothing
                            (SimilarLoaded ctx.accessGeneration id summary.id applied False)
                        , []
                        )

                Nothing ->
                    ( model, Cmd.none, [] )

        LoadMoreRequested ->
            case ( ( model.open, model.loading ), model.anchor, model.nextCursor ) of
                ( ( True, False ), Just summary, Just cursor ) ->
                    let
                        id =
                            model.requestId + 1
                    in
                    ( { model | loading = True, requestId = id, status = Just "Loading similar posts…" }
                    , Api.Similarity.listSimilar ctx.apiBase summary.id model.threshold pageLimit (Just cursor)
                        (SimilarLoaded ctx.accessGeneration id summary.id model.threshold True)
                    , []
                    )

                _ ->
                    ( model, Cmd.none, [] )

        ThresholdChanged value ->
            ( { model | thresholdInput = clampThreshold value }, Cmd.none, [] )

        SimilarLoaded generation id forAnchor appliedThreshold appending result ->
            if generation /= ctx.accessGeneration
                || id /= model.requestId
                || not model.open
                || anchorId model /= Just forAnchor
                || appliedThreshold /= model.threshold
            then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok page ->
                        if page.postId /= forAnchor || page.algorithm /= Domain.Similarity.algorithm then
                            ( { model | loading = False, status = Just "That similarity lookup returned an unexpected response. Use Reload to try again." }, Cmd.none, [] )

                        else
                            let
                                merged =
                                    if appending then
                                        mergeMatches model.matches page.matches

                                    else
                                        page.matches

                                pruned =
                                    Set.filter (\postId -> List.member postId (List.map (.id << .post) merged)) model.selected
                            in
                            ( { model
                                | matches = merged
                                , nextCursor = page.nextCursor
                                , indexed = page.indexed
                                , loading = False
                                , selected = pruned
                                , status =
                                    if page.indexed then
                                        Nothing

                                    else
                                        Just "This post has no similarity index yet, so no matches can be shown. Index this post to build it; new uploads are indexed automatically."
                              }
                            , Cmd.none
                            , []
                            )

                    Err (Http.BadStatus 404) ->
                        ( { model | anchor = Nothing, matches = [], nextCursor = Nothing, loading = False, selected = Set.empty, status = Just "That post is no longer available (it may have been trashed or hidden), so its similar posts cannot be shown. Close this review and pick another post." }
                        , Cmd.none
                        , []
                        )

                    Err error ->
                        ( { model | loading = False, status = Just ("Similar posts could not be loaded (HTTP " ++ httpStatus error ++ "). Use Reload to try again.") }, Cmd.none, [] )

        MatchToggled postId isChecked ->
            if model.loading || not model.open || not (List.member postId (List.map (.id << .post) model.matches)) then
                ( model, Cmd.none, [] )

            else if isChecked then
                if Set.member postId model.selected then
                    ( model, Cmd.none, [] )

                else if Set.size model.selected >= maxSelected then
                    ( { model | status = Just "At most 100 matches can be chosen at once. Move some to Trash or clear the choice first." }, Cmd.none, [] )

                else
                    ( { model | selected = Set.insert postId model.selected, status = Nothing }, Cmd.none, [] )

            else
                ( { model | selected = Set.remove postId model.selected }, Cmd.none, [] )

        SelectionCleared ->
            ( { model | selected = Set.empty }, Cmd.none, [] )

        CompareRequested candidateId ->
            case ( model.anchor, findMatch candidateId model.matches ) of
                ( Just anchorSummary, Just candidate ) ->
                    ( model, Cmd.none, [ CompareCopies anchorSummary candidate.post (anchorSummary :: members model) ] )

                _ ->
                    ( { model | status = Just "That match is no longer loaded. Reload before comparing." }, Cmd.none, [] )

        TrashRequested ->
            if model.loading || isBusy model then
                ( model, Cmd.none, [] )

            else if not ctx.writesEnabled then
                ( { model | status = Just "Unlock to move posts to Trash." }, Cmd.none, [] )

            else
                case trashableIds model of
                    [] ->
                        ( { model | status = Just "Choose at least one match with its checkbox before moving matches to Trash. Nothing is chosen automatically, even at distance 0." }, Cmd.none, [] )

                    ids ->
                        ( model, Cmd.none, [ TrashSelected ids ] )

        MediaFailed postId ->
            ( { model | missing = Set.insert postId model.missing }, Cmd.none, [] )

        IndexAnchorRequested ->
            case model.anchor of
                Just summary ->
                    if not ctx.writesEnabled then
                        ( { model | status = Just "Unlock to index this post. Browsing matches stays available while locked." }, Cmd.none, [] )

                    else if model.uncertain then
                        ( { model | status = Just "The last index request may have reached the server. Review it below before queueing again; nothing was re-queued." }, Cmd.none, [] )

                    else if model.posting || polling model || model.indexed then
                        ( model, Cmd.none, [] )

                    else
                        ( { model | posting = True, status = Just "Queueing index job…" }
                        , Api.Similarity.indexPosts ctx.apiBase ctx.credential [ summary.id ]
                            (IndexAnchorLoaded ctx.accessGeneration model.requestId)
                        , []
                        )

                Nothing ->
                    ( model, Cmd.none, [] )

        IndexAnchorLoaded generation reqId result ->
            if generation /= ctx.accessGeneration || reqId /= model.requestId || not model.open || not model.posting then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok response ->
                        case ( model.anchor, List.head response.posts ) of
                            ( Just summary, Just item ) ->
                                if item.id /= summary.id then
                                    ( { model | posting = False, status = Just "That index request returned an unexpected response. Use Reload to check again." }, Cmd.none, [] )

                                else
                                    let
                                        pid =
                                            model.pollId + 1
                                    in
                                    ( { model | posting = False, pollJob = Just item.jobId, pollId = pid, pollAttempts = 0, status = Just "Indexing this post… this can take a little while." }
                                    , schedulePoll ctx pid
                                    , []
                                    )

                            _ ->
                                ( { model | posting = False, status = Just "That index request returned an unexpected response. Use Reload to check again." }, Cmd.none, [] )

                    Err (Http.BadStatus 401) ->
                        ( { model | posting = False, status = Just "Your access was revoked. Unlock again to index posts." }, Cmd.none, [ AccessRevoked ] )

                    Err (Http.BadStatus 403) ->
                        ( { model | posting = False, status = Just "Your access was revoked. Unlock again to index posts." }, Cmd.none, [ AccessRevoked ] )

                    Err error ->
                        if isUncertain error then
                            ( { model | posting = False, uncertain = True, status = Just "The index request's outcome is uncertain — it may have been queued. Review before trying again; it was not retried automatically." }
                            , Cmd.none
                            , [ ResultUncertain "anchor index" ]
                            )

                        else
                            ( { model | posting = False, status = Just ("Indexing could not be queued (HTTP " ++ httpStatus error ++ "). Use Reload to check, then try again.") }, Cmd.none, [] )

        PollTick generation pid ->
            case model.pollJob of
                Just jobId ->
                    if generation /= ctx.accessGeneration || pid /= model.pollId || not model.open then
                        ( model, Cmd.none, [] )

                    else
                        ( model, Api.Job.get ctx.apiBase jobId (JobLoaded generation pid), [] )

                Nothing ->
                    ( model, Cmd.none, [] )

        JobLoaded generation pid result ->
            if generation /= ctx.accessGeneration || pid /= model.pollId || not model.open || model.pollJob == Nothing then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok job ->
                        if Api.Job.isTerminal job.status then
                            if job.status == "succeeded" then
                                reloadSimilar ctx { model | pollJob = Nothing }

                            else
                                ( { model | pollJob = Nothing, status = Just (failedJobMessage job) }, Cmd.none, [] )

                        else if model.pollAttempts + 1 >= maxPollAttempts then
                            ( { model | pollJob = Nothing, pollAttempts = model.pollAttempts + 1, status = Just "Indexing is taking longer than a minute. Use Reload later to refresh the results; no further automatic checks will run." }, Cmd.none, [] )

                        else
                            ( { model | pollAttempts = model.pollAttempts + 1, status = Just "Indexing this post… this can take a little while." }
                            , schedulePoll ctx pid
                            , []
                            )

                    Err _ ->
                        if model.pollAttempts + 1 >= maxPollAttempts then
                            ( { model | pollJob = Nothing, pollAttempts = model.pollAttempts + 1, status = Just "Indexing status could not be confirmed. Use Reload later to refresh the results." }, Cmd.none, [] )

                        else
                            ( { model | pollAttempts = model.pollAttempts + 1 }, schedulePoll ctx pid, [] )

        IndexLibraryRequested ->
            if not ctx.writesEnabled then
                ( { model | status = Just "Unlock to build the similarity index. Browsing matches stays available while locked." }, Cmd.none, [] )

            else if model.uncertain then
                ( { model | status = Just "The last index request may have reached the server. Review it below before queueing again; nothing was re-queued." }, Cmd.none, [] )

            else if indexingLibrary model then
                ( model, Cmd.none, [] )

            else
                let
                    gen =
                        model.queueGeneration + 1
                in
                ( { model | libraryPhase = LibraryScanning, queueGeneration = gen, missingQueue = [], queuedCount = 0, missingTotal = 0, status = Just "Checking which posts still need a similarity index…" }
                , Api.Similarity.getStatus ctx.apiBase ctx.credential batchLimit Nothing (StatusScanLoaded gen)
                , []
                )

        StatusScanLoaded gen result ->
            if gen /= model.queueGeneration || model.libraryPhase /= LibraryScanning || not model.open then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok page ->
                        let
                            missing =
                                page.posts
                                    |> List.filter (\item -> not item.indexed && item.status == Nothing && item.jobId == Nothing)
                                    |> List.map .id

                            queue =
                                model.missingQueue ++ missing

                            updated =
                                { model | indexedCount = page.indexedCount, eligibleCount = page.eligibleCount, missingQueue = queue, missingTotal = List.length queue }
                        in
                        case page.nextCursor of
                            Just cursor ->
                                ( updated, Api.Similarity.getStatus ctx.apiBase ctx.credential batchLimit (Just cursor) (StatusScanLoaded gen), [] )

                            Nothing ->
                                startNextBatch ctx gen updated

                    Err (Http.BadStatus 401) ->
                        ( { model | libraryPhase = LibraryIdle, status = Just "Your access was revoked. Unlock again to build the index." }, Cmd.none, [ AccessRevoked ] )

                    Err (Http.BadStatus 403) ->
                        ( { model | libraryPhase = LibraryIdle, status = Just "Your access was revoked. Unlock again to build the index." }, Cmd.none, [ AccessRevoked ] )

                    Err error ->
                        ( { model | libraryPhase = LibraryIdle, status = Just ("Index status could not be loaded (HTTP " ++ httpStatus error ++ "). Nothing was queued.") }, Cmd.none, [] )

        IndexBatchLoaded gen batch result ->
            if gen /= model.queueGeneration || model.libraryPhase /= LibraryQueuing || not model.open then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok _ ->
                        startNextBatch ctx gen { model | queuedCount = model.queuedCount + List.length batch, missingQueue = List.drop (List.length batch) model.missingQueue }

                    Err (Http.BadStatus 401) ->
                        ( { model | libraryPhase = LibraryIdle, status = Just "Your access was revoked. Already-queued jobs remain; unlock again to continue." }, Cmd.none, [ AccessRevoked ] )

                    Err (Http.BadStatus 403) ->
                        ( { model | libraryPhase = LibraryIdle, status = Just "Your access was revoked. Already-queued jobs remain; unlock again to continue." }, Cmd.none, [ AccessRevoked ] )

                    Err error ->
                        if isUncertain error then
                            ( { model | libraryPhase = LibraryIdle, uncertain = True, status = Just "A batch's outcome is uncertain — some posts may have been queued. Review before queueing again; it was not retried automatically." }
                            , Cmd.none
                            , [ ResultUncertain "library index batch" ]
                            )

                        else
                            ( { model | libraryPhase = LibraryIdle, status = Just ("A batch could not be queued (HTTP " ++ httpStatus error ++ "). Already-queued jobs remain.") }, Cmd.none, [] )

        StatusCountsLoaded gen result ->
            if gen /= model.queueGeneration || not model.open then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok page ->
                        ( { model | indexedCount = page.indexedCount, eligibleCount = page.eligibleCount }, Cmd.none, [] )

                    Err _ ->
                        ( model, Cmd.none, [] )

        CancelIndexLibraryRequested ->
            ( { model
                | libraryPhase = LibraryIdle
                , missingQueue = []
                , queueGeneration = model.queueGeneration + 1
                , status = Just "Stopped queueing further posts. Jobs already queued will still run; use Reload later to see updated matches."
              }
            , Cmd.none
            , []
            )

        UncertainReviewed ->
            ( { model | uncertain = False, status = Just "Thanks — you can queue indexing again when ready. Use Reload first if you want to check current results." }, Cmd.none, [] )

        TrashApplied ids ->
            if List.member (Maybe.withDefault "" (anchorId model)) ids then
                ( { model | anchor = Nothing, matches = [], nextCursor = Nothing, selected = Set.empty, status = Just "That post moved to Trash, so this review closed. Recover it from Trash if needed." }, Cmd.none, [] )

            else
                let
                    pruned =
                        { model
                            | matches = List.filter (\match -> not (List.member match.post.id ids)) model.matches
                            , selected = Set.filter (\postId -> not (List.member postId ids)) model.selected
                        }
                in
                if model.open && pruned.anchor /= Nothing then
                    reloadSimilar ctx pruned

                else
                    ( pruned, Cmd.none, [] )

        RestoreApplied _ ->
            if model.open && model.anchor /= Nothing then
                reloadSimilar ctx model

            else
                ( model, Cmd.none, [] )

        VisibilityChanged ->
            if model.open && model.anchor /= Nothing && not model.loading then
                reloadSimilar ctx model

            else
                ( model, Cmd.none, [] )


failedJobMessage : Api.Job.Job -> String
failedJobMessage job =
    let
        detail =
            if String.isEmpty (String.trim job.lastError) then
                ""

            else
                " (" ++ String.trim job.lastError ++ ")"
    in
    if job.status == "failed" then
        "Indexing this post failed"
            ++ detail
            ++ ". It was not retried automatically. After fixing the cause, Index this post to try again."

    else
        "Indexing this post did not finish ("
            ++ job.status
            ++ ")"
            ++ detail
            ++ ". It was not retried automatically. After fixing the cause, Index this post to try again."


clampThreshold : Int -> Int
clampThreshold value =
    clamp 0 16 value


isUncertain : Http.Error -> Bool
isUncertain error =
    case error of
        Http.Timeout ->
            True

        Http.NetworkError ->
            True

        _ ->
            False


schedulePoll : Context -> Int -> Cmd Msg
schedulePoll ctx pid =
    Process.sleep 2000 |> Task.perform (\_ -> PollTick ctx.accessGeneration pid)


reloadSimilar : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
reloadSimilar ctx model =
    case model.anchor of
        Just summary ->
            let
                id =
                    model.requestId + 1
            in
            ( { model | loading = True, requestId = id, status = Just "Loading similar posts…" }
            , Api.Similarity.listSimilar ctx.apiBase summary.id model.threshold pageLimit Nothing
                (SimilarLoaded ctx.accessGeneration id summary.id model.threshold False)
            , []
            )

        Nothing ->
            ( model, Cmd.none, [] )


startNextBatch : Context -> Int -> Model -> ( Model, Cmd Msg, List OutMsg )
startNextBatch ctx gen model =
    case List.take batchLimit model.missingQueue of
        [] ->
            ( { model | libraryPhase = LibraryIdle, status = Just ("Index queueing finished: " ++ String.fromInt model.queuedCount ++ " post(s) queued out of " ++ String.fromInt model.missingTotal ++ " missing. Matches update as jobs finish — use Reload later to refresh.") }
            , Api.Similarity.getStatus ctx.apiBase ctx.credential batchLimit Nothing (StatusCountsLoaded gen)
            , []
            )

        batch ->
            ( { model | libraryPhase = LibraryQueuing, status = Just ("Queueing similarity index jobs… " ++ String.fromInt model.queuedCount ++ " of " ++ String.fromInt model.missingTotal ++ " queued.") }
            , Api.Similarity.indexPosts ctx.apiBase ctx.credential batch (IndexBatchLoaded gen batch)
            , []
            )


mergeMatches : List SimilarMatch -> List SimilarMatch -> List SimilarMatch
mergeMatches existing incoming =
    existing ++ List.filter (\match -> not (List.member match.post.id (List.map (.id << .post) existing))) incoming


findMatch : String -> List SimilarMatch -> Maybe SimilarMatch
findMatch postId list =
    List.filter (\match -> match.post.id == postId) list |> List.head


httpStatus : Http.Error -> String
httpStatus error =
    case error of
        Http.BadStatus code ->
            String.fromInt code

        _ ->
            "network"


type alias ViewConfig msg =
    { apiBase : String
    , writesEnabled : Bool
    , onClose : msg
    , onReload : msg
    , onLoadMore : msg
    , onThresholdChanged : String -> msg
    , onToggle : String -> Bool -> msg
    , onClearSelection : msg
    , onCompare : String -> msg
    , onTrash : msg
    , onIndexAnchor : msg
    , onIndexLibrary : msg
    , onCancelIndexLibrary : msg
    , onReviewUncertain : msg
    , onMediaError : String -> msg
    }


view : Model -> ViewConfig msg -> Html msg
view model config =
    let
        chosenCount =
            List.length (selectedIds model)

        trashDisabled =
            not config.writesEnabled || chosenCount == 0 || isBusy model
    in
    div [ class "panel-section similarity-view" ]
        ([ p [ class "panel-note" ] [ text "Visual near-copies can be resized or recompressed versions of a post. They can also be different posts that merely look alike — even at distance 0 — so compare before moving anything to Trash. This is not the exact-copies check, and it does not match crops, flips, or meaning." ]
         , case model.status of
            Just message ->
                p [ class "panel-note", attribute "role" "status" ] [ text message ]

            Nothing ->
                text ""
         , case model.anchor of
            Just summary ->
                p [ class "panel-note" ] [ text ("Anchor #" ++ summary.id ++ " · " ++ String.fromInt (List.length model.matches) ++ " loaded · distance ≤ " ++ String.fromInt model.threshold) ]

            Nothing ->
                p [ class "panel-note" ] [ text "Pick a post to review its similar posts." ]
         ]
            ++ (if model.uncertain then
                    [ p []
                        [ button [ class "button button-quiet", type_ "button", onClick config.onReviewUncertain ] [ text "I reviewed the uncertain index request" ] ]
                    ]

                else
                    []
               )
            ++ [ ul [ class "nav-list similarity-matches" ]
                    (List.map (matchRow model config) model.matches)
               , if List.isEmpty model.matches && not model.loading then
                    p [ class "panel-note" ] [ text "No matches loaded." ]

                 else
                    text ""
               , p [ class "similarity-controls" ]
                    [ label [ class "similarity-threshold" ]
                        [ text "Max distance (0–16) "
                        , input [ type_ "number", Attr.min "0", Attr.max "16", value (String.fromInt model.thresholdInput), onInput config.onThresholdChanged ] []
                        ]
                    , button [ class "button button-quiet", type_ "button", onClick config.onReload, disabled model.loading ] [ text "Reload with this distance" ]
                    , button [ class "button button-quiet", type_ "button", onClick config.onLoadMore, disabled (model.loading || model.nextCursor == Nothing) ] [ text "Load more matches" ]
                    , button [ class "button button-quiet", type_ "button", onClick config.onClose ] [ text "Close" ]
                    ]
               , p [ class "similarity-controls" ]
                    [ button [ class "button button-quiet", type_ "button", onClick config.onIndexAnchor, disabled (not config.writesEnabled || model.indexed || model.posting || polling model || model.uncertain) ] [ text "Index this post" ]
                    , button [ class "button button-quiet", type_ "button", onClick config.onIndexLibrary, disabled (not config.writesEnabled || indexingLibrary model || model.uncertain) ] [ text "Build index for library" ]
                    , button [ class "button button-quiet", type_ "button", onClick config.onCancelIndexLibrary, disabled (not (indexingLibrary model)) ] [ text "Stop queueing" ]
                    , span [ class "panel-note" ] [ text (indexProgressText model config) ]
                    ]
               , p [ class "similarity-controls" ]
                    [ button [ class "button", type_ "button", onClick config.onTrash, disabled trashDisabled ] [ text ("Move chosen matches to Trash (" ++ String.fromInt chosenCount ++ "/100)") ]
                    , button [ class "button button-quiet", type_ "button", onClick config.onClearSelection, disabled (chosenCount == 0) ] [ text "Clear choice" ]
                    , span [ class "panel-note" ]
                        [ text
                            (if not config.writesEnabled then
                                " Unlock to move matches to Trash or build the index. Browsing stays available while locked."

                             else if chosenCount == 0 then
                                " Choose matches with their checkboxes first; nothing is chosen automatically. Trashed posts can be recovered from Trash."

                             else
                                " You will be asked to confirm before anything moves to Trash, and it can be recovered from Trash."
                            )
                        ]
                    ]
               ]
        )


indexProgressText : Model -> ViewConfig msg -> String
indexProgressText model _ =
    let
        ( indexed, eligible ) =
            indexCounts model
    in
    if indexingLibrary model then
        " Queueing: " ++ String.fromInt model.queuedCount ++ " of " ++ String.fromInt model.missingTotal ++ " missing queued so far. Jobs already queued will still run if you stop."

    else if eligible > 0 then
        " Indexed " ++ String.fromInt indexed ++ " of " ++ String.fromInt eligible ++ " eligible image posts. New uploads are indexed automatically; an existing library needs this explicit build."

    else
        " New uploads are indexed automatically; an existing library needs this explicit build."


matchRow : Model -> ViewConfig msg -> SimilarMatch -> Html msg
matchRow model config match =
    let
        post =
            match.post
    in
    li [ class "nav-row similarity-match-row" ]
        [ label [ class "similarity-match-check" ]
            [ input
                [ type_ "checkbox"
                , checked (Set.member post.id model.selected)
                , onCheck (config.onToggle post.id)
                , attribute "aria-label" ("Choose match " ++ post.id ++ " for Trash")
                ]
                []
            ]
        , Ui.Media.image [ class "similarity-thumb" ]
            { url = Api.Post.mediaUrl config.apiBase post.previewUrl
            , postId = post.id
            , missing = Set.member post.id model.missing
            , onError = config.onMediaError
            }
        , span [ class "similarity-match-meta" ]
            [ span [] [ text ("#" ++ post.id ++ " · distance " ++ String.fromInt match.distance ++ " · " ++ post.mediaType ++ " · " ++ String.fromInt post.width ++ "×" ++ String.fromInt post.height) ]
            , span [ class "panel-note" ] [ text "Visually similar — may still be a different post. Compare before Trash." ]
            ]
        , button [ class "button button-quiet", type_ "button", onClick (config.onCompare post.id) ] [ text "Compare" ]
        ]
