module Feature.Ratings exposing
    ( Context
    , Display(..)
    , Model
    , Msg(..)
    , OutMsg(..)
    , TargetInfo
    , baseVersion
    , capturedTargets
    , conflict
    , context
    , display
    , draft
    , errorToString
    , hasUnknownVersion
    , init
    , knownRating
    , knownVersion
    , locked
    , noteDetail
    , noteSummary
    , ownerIds
    , reloadPending
    , requestId
    , saving
    , selectionChanged
    , status
    , update
    , validateReceipt
    )

{-| Content-rating editing for one post or a captured bulk selection.

Base versions are captured when the user establishes a draft (or when a
selection is first read). A save captures its target ids, base versions,
desired rating, request id, and access generation at that explicit save;
navigation or selection changes afterwards cannot retarget it. Unknown
versions (legacy reads without `rating_version`) never fall back to zero:
writes stay disabled until a proper read supplies a version.
-}

import Api.Error as ApiError
import Api.Ratings
import Dict exposing (Dict)
import Domain.Post exposing (PostDetail, PostSummary, RatingResponse, RatingResult, RatingTarget, summaryOfDetail)
import Domain.Rating as Rating exposing (Rating)
import Http
import Set exposing (Set)


type alias TargetInfo =
    { id : String
    , rating : Rating
    , version : Maybe Int
    }


type alias Model =
    { draft : Maybe Rating
    , ownerIds : List String
    , baseVersions : Dict String Int
    , baseRatings : Dict String Rating
    , knownVersions : Dict String Int
    , knownRatings : Dict String Rating
    , saving : Bool
    , pendingTargets : List RatingTarget
    , pendingRating : Maybe Rating
    , status : Maybe String
    , requestId : Int
    , conflict : Bool
    , awaitingReload : Set String
    }


type alias Context =
    { apiBase : String
    , credential : Maybe String
    , writesEnabled : Bool
    , accessGeneration : Int
    , targets : List TargetInfo
    }


context : String -> Maybe String -> Bool -> Int -> List TargetInfo -> Context
context apiBase credential writesEnabled accessGeneration targets =
    { apiBase = apiBase
    , credential = credential
    , writesEnabled = writesEnabled
    , accessGeneration = accessGeneration
    , targets = targets
    }


type Msg
    = DraftChosen Rating
    | SaveRequested
    | ReloadRequested
    | Completed Int Int (List RatingTarget) Rating (Result Http.Error RatingResponse)


type OutMsg
    = Saved RatingResponse (List String)
    | Uncertain (List String)
    | StaleResponse (List String)
    | AccessRevoked
    | ReloadTargets (List String)


type Display
    = NoTargets
    | Uniform Rating
    | Mixed


init : Model
init =
    { draft = Nothing
    , ownerIds = []
    , baseVersions = Dict.empty
    , baseRatings = Dict.empty
    , knownVersions = Dict.empty
    , knownRatings = Dict.empty
    , saving = False
    , pendingTargets = []
    , pendingRating = Nothing
    , status = Nothing
    , requestId = 0
    , conflict = False
    , awaitingReload = Set.empty
    }


draft : Model -> Maybe Rating
draft model =
    model.draft


saving : Model -> Bool
saving model =
    model.saving


status : Model -> Maybe String
status model =
    model.status


conflict : Model -> Bool
conflict model =
    model.conflict


requestId : Model -> Int
requestId model =
    model.requestId


ownerIds : Model -> List String
ownerIds model =
    model.ownerIds


capturedTargets : Model -> List RatingTarget
capturedTargets model =
    model.pendingTargets


reloadPending : Model -> Bool
reloadPending model =
    not (Set.isEmpty model.awaitingReload)


baseVersion : String -> Model -> Maybe Int
baseVersion postId model =
    Dict.get postId model.baseVersions


knownVersion : String -> Model -> Maybe Int
knownVersion postId model =
    Dict.get postId model.knownVersions


knownRating : String -> Model -> Maybe Rating
knownRating postId model =
    Dict.get postId model.knownRatings


hasUnknownVersion : List TargetInfo -> Bool
hasUnknownVersion targets =
    List.any (\target -> target.version == Nothing) targets


{-| What the selector should show for these summaries: the draft when it
belongs to exactly this selection, otherwise their uniform rating or Mixed.
-}
display : List PostSummary -> Model -> Display
display summaries model =
    if List.isEmpty summaries then
        NoTargets

    else if model.draft /= Nothing && model.ownerIds == List.map .id summaries then
        case model.draft of
            Just rating ->
                Uniform rating

            Nothing ->
                NoTargets

    else
        case List.map (effectiveSummary model) summaries of
            first :: rest ->
                if List.all (\summary -> summary.rating == first.rating) rest then
                    Uniform first.rating

                else
                    Mixed

            [] ->
                NoTargets


{-| The canonical rating/version pair for a summary: a known pair at a
higher version wins atomically over an older incoming pair.
-}
effectiveSummary : Model -> PostSummary -> PostSummary
effectiveSummary model summary =
    case ( Dict.get summary.id model.knownVersions, Dict.get summary.id model.knownRatings ) of
        ( Just knownVersion_, Just knownRating_ ) ->
            case summary.ratingVersion of
                Just incoming ->
                    if incoming >= knownVersion_ then
                        summary

                    else
                        { summary | rating = knownRating_, ratingVersion = Just knownVersion_ }

                Nothing ->
                    { summary | rating = knownRating_, ratingVersion = Just knownVersion_ }

        _ ->
            summary


effectiveTarget : Model -> TargetInfo -> TargetInfo
effectiveTarget model target =
    case ( Dict.get target.id model.knownVersions, Dict.get target.id model.knownRatings ) of
        ( Just knownVersion_, Just knownRating_ ) ->
            case target.version of
                Just incoming ->
                    if incoming >= knownVersion_ then
                        target

                    else
                        { target | rating = knownRating_, version = Just knownVersion_ }

                Nothing ->
                    { target | rating = knownRating_, version = Just knownVersion_ }

        _ ->
            target


{-| A receipt pair merged monotonically: a higher known pair is kept
whole instead of regressing to the receipt's older pair.
-}
effectiveReceipt : Model -> RatingResult -> RatingResult
effectiveReceipt model post =
    case ( Dict.get post.id model.knownVersions, Dict.get post.id model.knownRatings ) of
        ( Just knownVersion_, Just knownRating_ ) ->
            if knownVersion_ > post.version then
                { post | version = knownVersion_, rating = knownRating_ }

            else
                post

        _ ->
            post


locked : Model -> Model
locked model =
    { model | saving = False, pendingTargets = [], pendingRating = Nothing }


captureBase : List TargetInfo -> ( Dict String Int, Dict String Rating )
captureBase targets =
    ( targets
        |> List.filterMap (\target -> target.version |> Maybe.map (\version -> ( target.id, version )))
        |> Dict.fromList
    , targets |> List.map (\target -> ( target.id, target.rating )) |> Dict.fromList
    )


noteSummary : PostSummary -> Model -> Model
noteSummary summary model =
    case summary.ratingVersion of
        Just version ->
            case Dict.get summary.id model.knownVersions of
                Just known ->
                    if version < known then
                        -- An older read cannot undo a newer acknowledgement.
                        model

                    else
                        noteFresh summary version model

                Nothing ->
                    noteFresh summary version model

        Nothing ->
            -- A legacy read without a version never invents one. When a
            -- version is already known, both members of that pair survive:
            -- the legacy rating (unrated when absent) must not be paired
            -- with the known version.
            case Dict.get summary.id model.knownVersions of
                Just _ ->
                    model

                Nothing ->
                    if model.draft /= Nothing && List.member summary.id model.ownerIds then
                        model

                    else
                        { model | knownRatings = Dict.insert summary.id summary.rating model.knownRatings }


noteFresh : PostSummary -> Int -> Model -> Model
noteFresh summary version model =
    let
        noted =
            { model
                | knownVersions = Dict.insert summary.id version model.knownVersions
                , knownRatings = Dict.insert summary.id summary.rating model.knownRatings
            }
    in
    if Set.member summary.id model.awaitingReload then
        let
            awaiting =
                Set.remove summary.id noted.awaitingReload
        in
        { noted
            | baseVersions = Dict.insert summary.id version noted.baseVersions
            , baseRatings = Dict.insert summary.id summary.rating noted.baseRatings
            , awaitingReload = awaiting
            , conflict =
                if Set.isEmpty awaiting then
                    False

                else
                    noted.conflict
            , status =
                if Set.isEmpty awaiting then
                    Just "Ratings reloaded. Review the rating before saving."

                else
                    noted.status
        }

    else if model.draft /= Nothing && List.member summary.id model.ownerIds then
        if Dict.get summary.id model.baseVersions /= Just version then
            -- Background refresh: the draft and its base stay put.
            { noted | conflict = True }

        else
            noted

    else if model.draft == Nothing && List.member summary.id model.ownerIds then
        { noted
            | baseVersions = Dict.insert summary.id version noted.baseVersions
            , baseRatings = Dict.insert summary.id summary.rating noted.baseRatings
        }

    else
        noted


noteDetail : PostDetail -> Model -> Model
noteDetail detail model =
    noteSummary (summaryOfDetail detail) model


selectionChanged : List PostSummary -> Model -> Model
selectionChanged summaries model =
    let
        ids =
            List.map .id summaries

        noted =
            List.foldl noteSummary model summaries

        effective =
            List.map (effectiveSummary noted) summaries
    in
    if ids == model.ownerIds then
        if model.draft == Nothing then
            let
                ( versions, ratings ) =
                    captureBase (List.map summaryTarget effective)
            in
            { noted | baseVersions = versions, baseRatings = ratings }

        else
            noted

    else
        let
            ( versions, ratings ) =
                captureBase (List.map summaryTarget effective)
        in
        { noted
            | ownerIds = ids
            , draft = Nothing
            , baseVersions = versions
            , baseRatings = ratings
            , conflict = False
            , status = Nothing
            , awaitingReload = Set.empty
        }


summaryTarget : PostSummary -> TargetInfo
summaryTarget summary =
    { id = summary.id, rating = summary.rating, version = summary.ratingVersion }


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        DraftChosen rating ->
            let
                ids =
                    List.map .id ctx.targets

                ( versions, ratings ) =
                    if model.draft /= Nothing && model.ownerIds == ids then
                        ( model.baseVersions, model.baseRatings )

                    else
                        captureBase (List.map (effectiveTarget model) ctx.targets)
            in
            ( { model
                | draft = Just rating
                , ownerIds = ids
                , baseVersions = versions
                , baseRatings = ratings
                , status = Nothing
              }
            , Cmd.none
            , []
            )

        SaveRequested ->
            save ctx model

        ReloadRequested ->
            if List.isEmpty model.ownerIds then
                ( model, Cmd.none, [] )

            else
                ( { model | awaitingReload = Set.fromList model.ownerIds, status = Just "Reloading the selected posts…" }
                , Cmd.none
                , [ ReloadTargets model.ownerIds ]
                )

        Completed generation id captured desired result ->
            if generation /= ctx.accessGeneration then
                ( model, Cmd.none, [ StaleResponse (List.map .id captured) ] )

            else if id /= model.requestId then
                ( model, Cmd.none, [] )

            else
                settle captured desired result model


settle : List RatingTarget -> Rating -> Result Http.Error RatingResponse -> Model -> ( Model, Cmd Msg, List OutMsg )
settle captured desired result model =
    case result of
        Ok response ->
            if validateReceipt captured desired response then
                let
                    effectivePosts =
                        List.map (effectiveReceipt model) response.posts

                    knownVersions =
                        List.foldl (\post dict -> Dict.insert post.id post.version dict) model.knownVersions effectivePosts

                    knownRatings =
                        List.foldl (\post dict -> Dict.insert post.id post.rating dict) model.knownRatings effectivePosts

                    capturedIds =
                        List.map .id captured

                    ownsDraft =
                        model.ownerIds == capturedIds && model.draft == Just desired

                    staleAck =
                        List.any
                            (\post ->
                                Dict.get post.id model.knownVersions
                                    |> Maybe.map (\known -> known > post.version)
                                    |> Maybe.withDefault False
                            )
                            response.posts

                    stillDirty =
                        ownsDraft && staleAck && List.any (\post -> post.rating /= desired) effectivePosts
                in
                ( { model
                    | knownVersions = knownVersions
                    , knownRatings = knownRatings
                    , saving = False
                    , pendingTargets = []
                    , pendingRating = Nothing
                    , draft =
                        if ownsDraft && not stillDirty then
                            Nothing

                        else
                            model.draft
                    , baseVersions =
                        if ownsDraft && not stillDirty then
                            List.foldl (\post dict -> Dict.insert post.id post.version dict) model.baseVersions effectivePosts

                        else
                            model.baseVersions
                    , baseRatings =
                        if ownsDraft && not stillDirty then
                            List.foldl (\post dict -> Dict.insert post.id post.rating dict) model.baseRatings effectivePosts

                        else
                            model.baseRatings
                    , conflict =
                        if stillDirty then
                            True

                        else if ownsDraft then
                            False

                        else
                            model.conflict
                    , status =
                        if stillDirty then
                            Just "Rating saved, but a newer rating is now shown. Your draft is kept; reload the selected posts before saving again."

                        else if ownsDraft then
                            Just "Rating saved."

                        else
                            model.status
                  }
                , Cmd.none
                , [ Saved response capturedIds ]
                )

            else
                ( { model
                    | saving = False
                    , pendingTargets = []
                    , pendingRating = Nothing
                    , status = Just "The rating save result is uncertain: the server response did not match the request. The selected posts were refreshed; check the rating before saving again."
                  }
                , Cmd.none
                , [ Uncertain (List.map .id captured) ]
                )

        Err (Http.BadStatus 401) ->
            ( model, Cmd.none, [ AccessRevoked ] )

        Err (Http.BadStatus 403) ->
            ( { model | saving = False, pendingTargets = [], pendingRating = Nothing, status = Just "This credential cannot change content ratings." }
            , Cmd.none
            , []
            )

        Err (Http.BadStatus 409) ->
            ( { model
                | saving = False
                , pendingTargets = []
                , pendingRating = Nothing
                , conflict = True
                , status = Just "Content rating changed elsewhere. Your draft is kept; reload the selected posts before saving again."
              }
            , Cmd.none
            , []
            )

        Err (Http.BadStatus 404) ->
            ( { model | saving = False, pendingTargets = [], pendingRating = Nothing, status = Just "One selected post no longer exists or is not visible." }
            , Cmd.none
            , []
            )

        Err (Http.BadStatus 400) ->
            ( { model | saving = False, pendingTargets = [], pendingRating = Nothing, status = Just "The rating request was invalid." }
            , Cmd.none
            , []
            )

        Err error ->
            case ApiError.classify error of
                ApiError.BadStatus code ->
                    if code >= 500 then
                        uncertain captured error model

                    else
                        ( { model | saving = False, pendingTargets = [], pendingRating = Nothing, status = Just (errorToString error) }
                        , Cmd.none
                        , []
                        )

                _ ->
                    uncertain captured error model


uncertain : List RatingTarget -> Http.Error -> Model -> ( Model, Cmd Msg, List OutMsg )
uncertain captured error model =
    ( { model
        | saving = False
        , pendingTargets = []
        , pendingRating = Nothing
        , status = Just ("The rating save result is uncertain: " ++ uncertainReason error ++ " The selected posts were refreshed; check the rating before saving again.")
      }
    , Cmd.none
    , [ Uncertain (List.map .id captured) ]
    )


uncertainReason : Http.Error -> String
uncertainReason error =
    errorToString error


save : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
save ctx model =
    if model.saving then
        ( model, Cmd.none, [] )

    else if not ctx.writesEnabled then
        ( { model | status = Just "Unlock to change content ratings." }, Cmd.none, [] )

    else if model.conflict || not (Set.isEmpty model.awaitingReload) then
        ( { model | status = Just "Reload the selected posts before saving this rating draft." }, Cmd.none, [] )

    else
        case model.draft of
            Nothing ->
                ( { model | status = Just "Choose a content rating before saving." }, Cmd.none, [] )

            Just desired ->
                if List.isEmpty model.ownerIds then
                    ( { model | status = Just "Select a post before changing its content rating." }, Cmd.none, [] )

                else if List.length model.ownerIds > 100 then
                    ( { model | status = Just "Select at most 100 posts to change ratings at once." }, Cmd.none, [] )

                else
                    let
                        targets =
                            List.map (\id -> Dict.get id model.baseVersions |> Maybe.map (RatingTarget id)) model.ownerIds
                    in
                    if List.any (\target -> target == Nothing) targets then
                        ( { model | status = Just "A content rating version is unknown for one selected post. Reload the selected posts before saving." }, Cmd.none, [] )

                    else
                        let
                            captured =
                                List.filterMap identity targets

                            id =
                                model.requestId + 1
                        in
                        ( { model
                            | saving = True
                            , status = Nothing
                            , conflict = False
                            , requestId = id
                            , pendingTargets = captured
                            , pendingRating = Just desired
                          }
                        , Api.Ratings.edit ctx.apiBase ctx.credential captured desired (Completed ctx.accessGeneration id captured desired)
                        , []
                        )


{-| A success receipt must name exactly the submitted ids (no extra,
missing, or duplicate ids), use canonical positive ids, return the
requested rating, and report `base + 1` for a change or `base` for a no-op.
Anything else is uncertain, never a completed save.
-}
validateReceipt : List RatingTarget -> Rating -> RatingResponse -> Bool
validateReceipt captured desired response =
    let
        submittedIds =
            List.map .id captured

        returnedIds =
            List.map .id response.posts
    in
    List.length response.posts
        == List.length captured
        && List.sort returnedIds
        == List.sort submittedIds
        && List.all
            (\post ->
                case List.filter (\target -> target.id == post.id) captured |> List.head of
                    Just target ->
                        canonicalPositive post.id
                            && post.rating
                            == desired
                            && post.version
                            == (if post.changed then
                                    target.version + 1

                                else
                                    target.version
                               )

                    Nothing ->
                        False
            )
            response.posts


canonicalPositive : String -> Bool
canonicalPositive id =
    case String.toInt id of
        Just value ->
            value > 0 && String.fromInt value == id

        Nothing ->
            False


errorToString : Http.Error -> String
errorToString error =
    case ApiError.classify error of
        ApiError.BadStatus 409 ->
            "Content rating changed elsewhere. Reload the selected posts and try again."

        ApiError.BadStatus 404 ->
            "One selected post no longer exists or is not visible."

        ApiError.BadStatus code ->
            "Rating edit failed (HTTP " ++ String.fromInt code ++ ")."

        ApiError.BadUrl url ->
            "Invalid API URL: " ++ url

        ApiError.Timeout ->
            "The rating edit timed out."

        ApiError.NetworkError ->
            "The rating edit server could not be reached."

        ApiError.BadBody ->
            "The rating response was invalid."
